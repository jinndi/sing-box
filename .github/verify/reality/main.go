package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"time"
)

const (
	mlkem768Group uint16 = 0x11ec
	x25519Group   uint16 = 0x001d
)

type clientHello struct {
	raw       []byte
	random    []byte
	sessionID []byte
	groups    []uint16
	shares    []keyShare
	alpns     []string
}

func main() {
	listenAddress := flag.String("listen", "127.0.0.1:18443", "address to listen on")
	fingerprint := flag.String("fingerprint", "chrome", "fingerprint being verified")
	privateKeyInput := flag.String("private-key", "", "REALITY server private key in base64url")
	readyFile := flag.String("ready-file", "", "file created when the listener is ready")
	flag.Parse()
	privateKey, err := base64.RawURLEncoding.DecodeString(*privateKeyInput)
	if err != nil || len(privateKey) != 32 {
		fail(errors.New("private key must be a 32-byte base64url value"))
	}

	listener, err := net.Listen("tcp", *listenAddress)
	if err != nil {
		fail(err)
	}
	defer listener.Close()
	if *readyFile != "" {
		if err := os.WriteFile(*readyFile, nil, 0600); err != nil {
			fail(err)
		}
	}

	conn, err := listener.Accept()
	if err != nil {
		fail(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	helloBytes, err := readClientHello(conn)
	if err != nil {
		fail(err)
	}
	hello, err := parseClientHello(helloBytes)
	if err != nil {
		fail(err)
	}
	version, err := openSessionID(privateKey, hello)
	if err != nil {
		fail(fmt.Errorf("%s fingerprint: session ID: %w", *fingerprint, err))
	}
	if version != [3]byte{26, 3, 27} {
		fail(fmt.Errorf("%s fingerprint: session ID does not identify client 26.3.27 (got %d.%d.%d)", *fingerprint, version[0], version[1], version[2]))
	}
	if err := validateClientHello(hello); err != nil {
		fail(fmt.Errorf("%s fingerprint: %w", *fingerprint, err))
	}
	fmt.Printf("%s REALITY ClientHello verified\n", *fingerprint)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func readClientHello(conn net.Conn) ([]byte, error) {
	var handshake []byte
	for len(handshake) < 4 {
		var header [5]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return nil, err
		}
		if header[0] != 22 {
			return nil, fmt.Errorf("expected TLS handshake record, got content type %d", header[0])
		}
		recordLength := int(binary.BigEndian.Uint16(header[3:]))
		if recordLength == 0 || recordLength > 1<<14 {
			return nil, fmt.Errorf("invalid TLS record length %d", recordLength)
		}
		record := make([]byte, recordLength)
		if _, err := io.ReadFull(conn, record); err != nil {
			return nil, err
		}
		handshake = append(handshake, record...)
	}
	if handshake[0] != 1 {
		return nil, fmt.Errorf("expected ClientHello handshake, got type %d", handshake[0])
	}
	handshakeLength := int(handshake[1])<<16 | int(handshake[2])<<8 | int(handshake[3])
	if handshakeLength > 1<<20 {
		return nil, errors.New("ClientHello exceeds size limit")
	}
	for len(handshake) < handshakeLength+4 {
		var header [5]byte
		if _, err := io.ReadFull(conn, header[:]); err != nil {
			return nil, err
		}
		if header[0] != 22 {
			return nil, fmt.Errorf("expected TLS handshake record, got content type %d", header[0])
		}
		recordLength := int(binary.BigEndian.Uint16(header[3:]))
		record := make([]byte, recordLength)
		if _, err := io.ReadFull(conn, record); err != nil {
			return nil, err
		}
		handshake = append(handshake, record...)
	}
	return handshake[:handshakeLength+4], nil
}

func parseClientHello(raw []byte) (clientHello, error) {
	hello := clientHello{raw: raw}
	var err error
	if len(raw) < 4 || raw[0] != 1 {
		return hello, errors.New("not a ClientHello")
	}
	body := raw[4:]
	offset := 0
	read := func(length int) ([]byte, error) {
		if length < 0 || offset+length > len(body) {
			return nil, errors.New("truncated ClientHello")
		}
		part := body[offset : offset+length]
		offset += length
		return part, nil
	}
	readUint16 := func() (uint16, error) {
		part, err := read(2)
		if err != nil {
			return 0, err
		}
		return binary.BigEndian.Uint16(part), nil
	}

	if _, err := read(2); err != nil {
		return hello, err
	}
	hello.random, err = read(32)
	if err != nil {
		return hello, err
	}
	sessionLength, err := read(1)
	if err != nil {
		return hello, err
	}
	hello.sessionID, err = read(int(sessionLength[0]))
	if err != nil {
		return hello, err
	}
	cipherLength, err := readUint16()
	if err != nil {
		return hello, err
	}
	if _, err := read(int(cipherLength)); err != nil {
		return hello, err
	}
	compressionLength, err := read(1)
	if err != nil {
		return hello, err
	}
	if _, err := read(int(compressionLength[0])); err != nil {
		return hello, err
	}
	extensionsLength, err := readUint16()
	if err != nil {
		return hello, err
	}
	extensions, err := read(int(extensionsLength))
	if err != nil {
		return hello, err
	}
	if offset != len(body) {
		return hello, errors.New("unexpected trailing data after ClientHello extensions")
	}

	for len(extensions) > 0 {
		if len(extensions) < 4 {
			return hello, errors.New("truncated TLS extension header")
		}
		extensionType := binary.BigEndian.Uint16(extensions[:2])
		extensionLength := int(binary.BigEndian.Uint16(extensions[2:4]))
		extensions = extensions[4:]
		if extensionLength > len(extensions) {
			return hello, errors.New("truncated TLS extension")
		}
		data := extensions[:extensionLength]
		extensions = extensions[extensionLength:]
		switch extensionType {
		case 10:
			hello.groups, err = parseGroups(data)
		case 16:
			hello.alpns, err = parseALPN(data)
		case 51:
			hello.shares, err = parseKeyShares(data)
		}
		if err != nil {
			return hello, err
		}
	}
	return hello, nil
}

func parseGroups(data []byte) ([]uint16, error) {
	if len(data) < 2 {
		return nil, errors.New("truncated supported-groups extension")
	}
	length := int(binary.BigEndian.Uint16(data[:2]))
	if length != len(data)-2 || length%2 != 0 {
		return nil, errors.New("invalid supported-groups list")
	}
	groups := make([]uint16, 0, length/2)
	for data = data[2:]; len(data) > 0; data = data[2:] {
		groups = append(groups, binary.BigEndian.Uint16(data[:2]))
	}
	return groups, nil
}

type keyShare struct {
	group uint16
	data  []byte
}

func parseKeyShares(data []byte) ([]keyShare, error) {
	if len(data) < 2 {
		return nil, errors.New("truncated key-share extension")
	}
	length := int(binary.BigEndian.Uint16(data[:2]))
	if length != len(data)-2 {
		return nil, errors.New("invalid key-share list")
	}
	shares := make([]keyShare, 0)
	for data = data[2:]; len(data) > 0; {
		if len(data) < 4 {
			return nil, errors.New("truncated key share")
		}
		group := binary.BigEndian.Uint16(data[:2])
		keyLength := int(binary.BigEndian.Uint16(data[2:4]))
		data = data[4:]
		if keyLength > len(data) {
			return nil, errors.New("truncated key-share key")
		}
		shares = append(shares, keyShare{group: group, data: data[:keyLength]})
		data = data[keyLength:]
	}
	return shares, nil
}

func parseALPN(data []byte) ([]string, error) {
	if len(data) < 2 {
		return nil, errors.New("truncated ALPN extension")
	}
	length := int(binary.BigEndian.Uint16(data[:2]))
	if length != len(data)-2 {
		return nil, errors.New("invalid ALPN protocol list")
	}
	protocols := make([]string, 0)
	for data = data[2:]; len(data) > 0; {
		protocolLength := int(data[0])
		data = data[1:]
		if protocolLength == 0 || protocolLength > len(data) {
			return nil, errors.New("invalid ALPN protocol")
		}
		protocols = append(protocols, string(data[:protocolLength]))
		data = data[protocolLength:]
	}
	return protocols, nil
}

func validateClientHello(hello clientHello) error {
	groups := make(map[uint16]bool, len(hello.groups))
	for _, group := range hello.groups {
		groups[group] = true
	}
	for _, share := range hello.shares {
		if !isGREASE(share.group) && !groups[share.group] {
			return fmt.Errorf("key-share group %04x is not in supported groups", share.group)
		}
	}
	hybridSeen := false
	for _, share := range hello.shares {
		if share.group == mlkem768Group && len(share.data) == 1184+32 {
			if hybridSeen {
				return errors.New("multiple X25519MLKEM768 key shares")
			}
			hybridSeen = true
			continue
		}
		if share.group == x25519Group && len(share.data) == 32 {
			break
		}
	}
	if !hybridSeen {
		return errors.New("no X25519MLKEM768 key share before the first X25519 key share")
	}
	if groups[mlkem768Group] && !hasShare(hello.shares, mlkem768Group) {
		return errors.New("X25519MLKEM768 is supported without a corresponding key share")
	}
	if !slices.Equal(hello.alpns, []string{"h2", "http/1.1"}) {
		return fmt.Errorf("ALPN is %q, want [h2 http/1.1]", hello.alpns)
	}
	return nil
}

func hasShare(shares []keyShare, group uint16) bool {
	for _, share := range shares {
		if share.group == group {
			return true
		}
	}
	return false
}

func isGREASE(group uint16) bool {
	return group&0x0f0f == 0x0a0a && group>>8 == group&0xff
}

func openSessionID(privateKey []byte, hello clientHello) ([3]byte, error) {
	var version [3]byte
	var peer []byte
	for _, share := range hello.shares {
		if share.group == x25519Group {
			peer = share.data
			break
		}
	}
	if len(peer) == 0 {
		for _, share := range hello.shares {
			if share.group == mlkem768Group && len(share.data) == 1184+32 {
				peer = share.data[1184:]
				break
			}
		}
	}
	if len(peer) != 32 {
		return version, errors.New("ClientHello has no usable X25519 public key share")
	}
	private, err := ecdh.X25519().NewPrivateKey(privateKey)
	if err != nil {
		return version, err
	}
	public, err := ecdh.X25519().NewPublicKey(peer)
	if err != nil {
		return version, err
	}
	shared, err := private.ECDH(public)
	if err != nil {
		return version, err
	}
	authKey, err := hkdf.Key(sha256.New, shared, hello.random[:20], "REALITY", 32)
	if err != nil {
		return version, err
	}
	block, err := aes.NewCipher(authKey)
	if err != nil {
		return version, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return version, err
	}
	if len(hello.random) != 32 || len(hello.sessionID) != 32 || len(hello.random[20:]) != aead.NonceSize() {
		return version, errors.New("invalid REALITY random or session ID size")
	}
	additionalData := append([]byte(nil), hello.raw...)
	clear(additionalData[39 : 39+len(hello.sessionID)])
	plaintext, err := aead.Open(nil, hello.random[20:], hello.sessionID, additionalData)
	if err != nil {
		return version, fmt.Errorf("cannot decrypt with the X25519 key share: %w", err)
	}
	if len(plaintext) < len(version) {
		return version, errors.New("decrypted session ID is too short")
	}
	copy(version[:], plaintext[:len(version)])
	return version, nil
}
