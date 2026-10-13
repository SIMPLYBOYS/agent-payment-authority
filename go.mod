module github.com/SIMPLYBOYS/agent-payment-authority

go 1.26.0

// modernc.org/sqlite requires downstream modules to pin the same
// modernc.org/libc version as its own go.mod (v1.77.1 for v1.60.1).
require modernc.org/sqlite v1.60.1

// P1-b's Signer: secp256k1 signing (RFC 6979, compact recoverable) and
// keccak-256 / scrypt; x/sys was already pinned and is now imported for the
// peer uid and the terminal (docs/commercial-p1b-design.md 9, decision 2).
require (
	github.com/decred/dcrd/dcrec/secp256k1/v4 v4.4.1
	golang.org/x/crypto v0.57.0
	golang.org/x/sys v0.48.0
)

// A2's human approval: WebAuthn registration and assertion verification,
// in the Signer only (docs/commercial-human-approval-design.md 4.8, D4).
require github.com/go-webauthn/webauthn v0.18.2

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fxamacker/cbor/v2 v2.9.4 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/go-webauthn/x v0.3.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/tinylib/msgp v1.6.4 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	modernc.org/libc v1.77.1 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)
