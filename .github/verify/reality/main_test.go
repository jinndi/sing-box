package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"net"
	"testing"
)

func TestParseAndValidateClientHello(t *testing.T) {
	body := testClientHello()
	hello, err := parseClientHello(body)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateClientHello(hello); err != nil {
		t.Fatal(err)
	}
}

func TestRejectsWrongKeyShareOrder(t *testing.T) {
	hello, err := parseClientHello(testClientHello())
	if err != nil {
		t.Fatal(err)
	}
	hello.shares[0], hello.shares[1] = hello.shares[1], hello.shares[0]
	if err := validateClientHello(hello); err == nil {
		t.Fatal("expected invalid key-share order to fail")
	}
}

func TestAllowsAdditionalShareBetweenHybridAndX25519(t *testing.T) {
	hello, err := parseClientHello(testClientHello())
	if err != nil {
		t.Fatal(err)
	}
	p256 := keyShare{group: 0x0017, data: make([]byte, 65)}
	hello.groups = append(hello.groups, p256.group)
	hello.shares = []keyShare{hello.shares[0], p256, hello.shares[1]}
	if err := validateClientHello(hello); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSessionID(t *testing.T) {
	serverPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hello, err := parseClientHello(testClientHello())
	if err != nil {
		t.Fatal(err)
	}
	hello.shares[1].data = clientPrivate.PublicKey().Bytes()
	shared, err := serverPrivate.ECDH(clientPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	authKey, err := hkdf.Key(sha256.New, shared, hello.random[:20], "REALITY", 32)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(authKey)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	additionalData := append([]byte(nil), hello.raw...)
	clear(additionalData[39 : 39+len(hello.sessionID)])
	plaintext := []byte{26, 3, 27, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}
	ciphertext := aead.Seal(nil, hello.random[20:], plaintext, additionalData)
	copy(hello.sessionID, ciphertext)

	version, err := openSessionID(serverPrivate.Bytes(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if version != [3]byte{26, 3, 27} {
		t.Fatalf("got session version %v, want [26 3 27]", version)
	}
}

func TestOpenSessionIDRejectsWrongVersion(t *testing.T) {
	serverPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hello, err := parseClientHello(testClientHello())
	if err != nil {
		t.Fatal(err)
	}
	hello.shares[1].data = clientPrivate.PublicKey().Bytes()
	shared, err := serverPrivate.ECDH(clientPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	authKey, err := hkdf.Key(sha256.New, shared, hello.random[:20], "REALITY", 32)
	if err != nil {
		t.Fatal(err)
	}
	block, err := aes.NewCipher(authKey)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	additionalData := append([]byte(nil), hello.raw...)
	clear(additionalData[39 : 39+len(hello.sessionID)])
	plaintext := []byte{1, 8, 1, 0, 0, 0, 0, 0, 1, 2, 3, 4, 5, 6, 7, 8}
	ciphertext := aead.Seal(nil, hello.random[20:], plaintext, additionalData)
	copy(hello.sessionID, ciphertext)

	version, err := openSessionID(serverPrivate.Bytes(), hello)
	if err != nil {
		t.Fatal(err)
	}
	if version == [3]byte{26, 3, 27} {
		t.Fatal("expected decrypted upstream version to differ from patched version")
	}
}

func TestReadFragmentedClientHello(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	body := testClientHello()
	firstRecord := append([]byte{22, 3, 1, 0, 2}, body[:2]...)
	secondRecord := append([]byte{22, 3, 1, byte((len(body) - 2) >> 8), byte(len(body) - 2)}, body[2:]...)
	go func() {
		_, _ = client.Write(firstRecord)
		_, _ = client.Write(secondRecord)
	}()

	readBody, err := readClientHello(server)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(readBody, body) {
		t.Fatal("read ClientHello message differs from sent message")
	}
}

func testClientHello() []byte {
	var extensions bytes.Buffer
	appendExtension(&extensions, 10, []byte{0, 4, 0x11, 0xec, 0, 0x1d})
	appendExtension(&extensions, 16, []byte{0, 12, 2, 'h', '2', 8, 'h', 't', 't', 'p', '/', '1', '.', '1'})
	var shares bytes.Buffer
	_ = binary.Write(&shares, binary.BigEndian, uint16(2+2+1184+32+2+2+32))
	_ = binary.Write(&shares, binary.BigEndian, mlkem768Group)
	_ = binary.Write(&shares, binary.BigEndian, uint16(1184+32))
	shares.Write(make([]byte, 1184+32))
	_ = binary.Write(&shares, binary.BigEndian, x25519Group)
	_ = binary.Write(&shares, binary.BigEndian, uint16(32))
	shares.Write(make([]byte, 32))
	appendExtension(&extensions, 51, shares.Bytes())

	var body bytes.Buffer
	body.Write([]byte{3, 3})
	body.Write(make([]byte, 32))
	sessionID := make([]byte, 32)
	copy(sessionID, []byte{26, 3, 27})
	body.WriteByte(byte(len(sessionID)))
	body.Write(sessionID)
	body.Write([]byte{0, 2, 0x13, 1, 1, 0})
	_ = binary.Write(&body, binary.BigEndian, uint16(extensions.Len()))
	body.Write(extensions.Bytes())
	bodyBytes := body.Bytes()
	message := []byte{1, byte(len(bodyBytes) >> 16), byte(len(bodyBytes) >> 8), byte(len(bodyBytes))}
	return append(message, bodyBytes...)
}

func appendExtension(out *bytes.Buffer, kind uint16, body []byte) {
	_ = binary.Write(out, binary.BigEndian, kind)
	_ = binary.Write(out, binary.BigEndian, uint16(len(body)))
	out.Write(body)
}
