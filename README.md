# sing-box for Keenetic / Netсraze

> [!IMPORTANT]
> If the server runs Xray version 26.9.8 or later and the `chrome` fingerprint does not work with REALITY, try the `randomized` fingerprint.

Prebuilt sing-box builds for Keenetic and Netсraze routers with additional REALITY-related patches.

This is not the upstream `sing-box` source code; it is a release project for building ready-to-use binaries for the router architectures commonly used in Keenetic / Netсraze devices. The goal is to build sing-box with the required tags and patches, validate the REALITY ClientHello, and publish the artifacts in a GitHub Release.

[Русская версия](README.ru.md)

## What this project is

The repository automatically:

- fetches the required upstream `SagerNet/sing-box` version by tag;
- applies the patch [.github/patches/reality.patch](.github/patches/reality.patch);
- builds binaries for the target architectures;
- adds the required `build tags`;
- optionally compresses binaries with `UPX`;
- verifies that the built binary passes the REALITY validation;
- publishes the results to GitHub Releases.

## Supported builds

GitHub Actions builds binaries for the following configurations:

- `linux/arm64` + `musl`
- `linux/mipsle` + `softfloat` + `musl`
- `linux/mips` + `softfloat`

For `arm64` and `mipsle`, the following tags are additionally enabled:

- `with_naive_outbound`
- `with_musl`

All builds use the common tag set:

- `with_quic`
- `with_utls`
- `with_clash_api`
- `badlinkname`
- `tfogo_checklinkname0`
- `with_gvisor` for stable releases

UPX-compressed variants are published with the `_upx` suffix when the compression check succeeds.

## Applied patches

1. The patch [.github/patches/reality.patch](.github/patches/reality.patch) is intended to keep REALITY compatible with Chrome/randomized fingerprints:

- preserves the `X25519MLKEM768` hybrid share for a `chrome`-compatible ClientHello;
- stabilizes `key share` and `ALPN` handling for the `randomized` fingerprint;
- corrects REALITY client-version identification;
- brings behavior closer to the expected Chrome/Xray client hello.

In other words, it solves the problem where upstream `sing-box`/`utls` produces a ClientHello without the required hybrid key or with a mismatched key layout, causing REALITY on Xray-based servers to reject the connection.

## Release validation

The workflow in [.github/workflows/build.yml](.github/workflows/build.yml) performs a two-step verification:

1. downloads the official upstream `sing-box` release for the selected version;
2. confirms that the unpatched baseline fails the REALITY validation;
3. builds the patched binary;
4. runs `.github/verify/reality/verify.sh` against it;
5. publishes only valid artifacts to GitHub Releases.

This makes the release process more reliable and prevents unsigned or non-compliant binaries from being published.

## License

This project is distributed under the MIT license. See [LICENSE](LICENSE).
