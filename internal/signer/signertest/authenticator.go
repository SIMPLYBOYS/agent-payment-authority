package signertest

// A software WebAuthn authenticator for tests (A2): an ES256 key made here,
// attestation fmt none (or a packed self attestation), authenticator data
// with the flags and counter a test sets. It is not a device and its
// results are never a real-device result.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator data flags.
const (
	UP = 0x01
	UV = 0x04
	BE = 0x08
	BS = 0x10
	AT = 0x40
)

var b64 = base64.RawURLEncoding

type Authenticator struct {
	Key    *ecdsa.PrivateKey
	ID     []byte
	Flags  byte   // UP|UV unless a test changes it
	Count  uint32 // the signature counter put in the next response
	RPID   string // localhost unless a test changes it
	Packed bool   // a packed self attestation instead of fmt none
	Handle []byte // the user handle of the creation options, sent back by Get
}

func NewAuthenticator() *Authenticator {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	id := make([]byte, 16)
	rand.Read(id)
	return &Authenticator{Key: k, ID: id, Flags: UP | UV, RPID: "localhost"}
}

func (a *Authenticator) authData(extra []byte, flags byte) []byte {
	rp := sha256.Sum256([]byte(a.RPID))
	ad := append(rp[:], flags)
	ad = binary.BigEndian.AppendUint32(ad, a.Count)
	return append(ad, extra...)
}

// ClientData is client data JSON of type typ; extra adds or replaces keys
// (crossOrigin, topOrigin).
func ClientData(typ, challenge, origin string, extra map[string]any) []byte {
	m := map[string]any{"type": typ, "challenge": challenge, "origin": origin}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.Marshal(m)
	return b
}

// Challenge reads publicKey.challenge (and the user handle, for a
// creation) from options as the Signer gave them.
func Challenge(options json.RawMessage) (string, []byte) {
	var o struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			User      struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	json.Unmarshal(options, &o)
	h, _ := b64.DecodeString(o.PublicKey.User.ID)
	return o.PublicKey.Challenge, h
}

// Create answers creation options as navigator.credentials.create would.
func (a *Authenticator) Create(options json.RawMessage, origin string) json.RawMessage {
	ch, handle := Challenge(options)
	a.Handle = handle
	pub, _ := a.Key.PublicKey.ECDH()
	raw := pub.Bytes() // 0x04 || X || Y
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: raw[1:33], -3: raw[33:]})
	att := make([]byte, 16) // AAGUID zero, as attestation none gives
	att = binary.BigEndian.AppendUint16(att, uint16(len(a.ID)))
	att = append(append(att, a.ID...), cose...)
	ad := a.authData(att, a.Flags|AT)
	cd := ClientData("webauthn.create", ch, origin, nil)
	format, stmt := "none", map[string]any{}
	if a.Packed {
		h := sha256.Sum256(cd)
		d := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
		sig, _ := ecdsa.SignASN1(rand.Reader, a.Key, d[:])
		format, stmt = "packed", map[string]any{"alg": -7, "sig": sig}
	}
	obj, _ := cbor.Marshal(map[string]any{"fmt": format, "attStmt": stmt, "authData": ad})
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.ID), "rawId": b64.EncodeToString(a.ID), "type": "public-key",
		"clientExtensionResults": map[string]any{}, "authenticatorAttachment": "platform",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "attestationObject": b64.EncodeToString(obj), "transports": []string{"internal"}}})
	return b
}

// Get answers request options as navigator.credentials.get would.
func (a *Authenticator) Get(options json.RawMessage, origin string) json.RawMessage {
	ch, _ := Challenge(options)
	return a.GetData(ClientData("webauthn.get", ch, origin, nil))
}

// GetData signs the given client data JSON: a test makes it wrong.
func (a *Authenticator) GetData(cd []byte) json.RawMessage {
	ad := a.authData(nil, a.Flags)
	h := sha256.Sum256(cd)
	d := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, _ := ecdsa.SignASN1(rand.Reader, a.Key, d[:])
	resp := map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad), "signature": b64.EncodeToString(sig)}
	if a.Handle != nil {
		resp["userHandle"] = b64.EncodeToString(a.Handle)
	}
	b, _ := json.Marshal(map[string]any{"id": b64.EncodeToString(a.ID), "rawId": b64.EncodeToString(a.ID), "type": "public-key",
		"clientExtensionResults": map[string]any{}, "authenticatorAttachment": "platform", "response": resp})
	return b
}

// ApprovalsConfig is Config with an approver per consumer (approver-a and
// approver-b, version 1, their own wallet) and the relying party
// localhost with origin (http://localhost:<port>).
func ApprovalsConfig(socket string, caller uint32, now uint64, addrA, addrB, origin string) []byte {
	var c map[string]any
	json.Unmarshal(Config(socket, caller, now, addrA, addrB), &c)
	c["approvers"] = []map[string]any{
		{"approver_id": "approver-a", "consumer_id": "consumer-a", "wallet_ids": []string{"wallet-a"}, "signer_max": "1000000", "version": 1},
		{"approver_id": "approver-b", "consumer_id": "consumer-b", "wallet_ids": []string{"wallet-b"}, "signer_max": "1000000", "version": 1},
	}
	c["webauthn"] = map[string]any{"rp_id": "localhost", "origins": []string{origin}, "challenge_ttl_seconds": 120, "approval_ttl_seconds": 300}
	raw, _ := json.Marshal(c)
	return raw
}
