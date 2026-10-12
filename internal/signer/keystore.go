package signer

// Encrypted key files (docs/commercial-p1b-design.md 3.3): one file per
// wallet, format version 1, scrypt then AES-256-GCM. The associated data
// binds the file to its wallet and address, so a file swapped between
// wallets or edited does not decrypt. A wrong passphrase, a changed byte or
// parameters outside the bounds are refused; there is no plaintext form and
// no other key source.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/crypto/scrypt"
)

// KeyFile is the on-disk form of one wallet's key.
type KeyFile struct {
	Version    int    `json:"version"` // 1
	WalletID   string `json:"wallet_id"`
	Address    string `json:"address"`
	KDF        string `json:"kdf"` // scrypt
	N          int    `json:"n"`
	R          int    `json:"r"`
	P          int    `json:"p"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// ScryptN is the work factor new key files get; DecryptKey accepts
// 2^14 to 2^20 with r=8, p=1, nothing else.
const ScryptN = 1 << 17

func aad(walletID, address string) []byte {
	return []byte("apa-signer-key/v1\x00" + walletID + "\x00" + address)
}

func gcmFor(pass []byte, salt []byte, n int) (cipher.AEAD, error) {
	k, err := scrypt.Key(pass, salt, n, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// EncryptKey makes a version 1 key file for a 32-byte private key.
func EncryptKey(walletID string, priv, pass []byte, n int) (KeyFile, error) {
	if len(priv) != 32 || len(pass) == 0 {
		return KeyFile{}, errors.New("a 32-byte key and a passphrase are required")
	}
	addr := Address(secp256k1.PrivKeyFromBytes(priv).PubKey())
	salt, nonce := make([]byte, 16), make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return KeyFile{}, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return KeyFile{}, err
	}
	g, err := gcmFor(pass, salt, n)
	if err != nil {
		return KeyFile{}, err
	}
	return KeyFile{Version: 1, WalletID: walletID, Address: addr, KDF: "scrypt", N: n, R: 8, P: 1, Salt: hex.EncodeToString(salt),
		Nonce: hex.EncodeToString(nonce), Ciphertext: hex.EncodeToString(g.Seal(nil, nonce, priv, aad(walletID, addr)))}, nil
}

// DecryptKey opens a key file for walletID and checks the key gives the
// file's address.
func DecryptKey(f KeyFile, walletID string, pass []byte) (*secp256k1.PrivateKey, error) {
	if f.Version != 1 || f.KDF != "scrypt" || f.R != 8 || f.P != 1 || f.N < 1<<14 || f.N > 1<<20 || f.N&(f.N-1) != 0 || f.WalletID != walletID {
		return nil, errors.New("key file: unsupported version, parameters or wallet")
	}
	salt, err1 := hex.DecodeString(f.Salt)
	nonce, err2 := hex.DecodeString(f.Nonce)
	ct, err3 := hex.DecodeString(f.Ciphertext)
	if errors.Join(err1, err2, err3) != nil || len(salt) != 16 || len(nonce) != 12 {
		return nil, errors.New("key file: malformed")
	}
	g, err := gcmFor(pass, salt, f.N)
	if err != nil {
		return nil, err
	}
	priv, err := g.Open(nil, nonce, ct, aad(walletID, f.Address))
	if err != nil || len(priv) != 32 {
		return nil, errors.New("key file: wrong passphrase or changed content")
	}
	key := secp256k1.PrivKeyFromBytes(priv)
	clear(priv)
	if Address(key.PubKey()) != f.Address {
		return nil, errors.New("key file: key does not give its address")
	}
	return key, nil
}

// readKeyFile reads one key file, which must be a regular file readable by
// its owner only.
func readKeyFile(path string) (KeyFile, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return KeyFile{}, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return KeyFile{}, fmt.Errorf("%s must be a regular file private to its owner", filepath.Base(path))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return KeyFile{}, err
	}
	var f KeyFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return KeyFile{}, fmt.Errorf("%s: not a key file", filepath.Base(path))
	}
	return f, nil
}

// LoadKeys decrypts each configured wallet's key file <dir>/<wallet_id>.key
// with the passphrase; the key must give the configured address.
func LoadKeys(dir string, cfg *Config, pass []byte) (map[string]*secp256k1.PrivateKey, error) {
	keys := map[string]*secp256k1.PrivateKey{}
	for _, w := range cfg.Wallets {
		f, err := readKeyFile(filepath.Join(dir, w.WalletID+".key"))
		if err != nil {
			return nil, err
		}
		k, err := DecryptKey(f, w.WalletID, pass)
		if err != nil {
			return nil, fmt.Errorf("wallet %s: %w", w.WalletID, err)
		}
		if f.Address != w.Address {
			return nil, fmt.Errorf("wallet %s: the key is not the configured address", w.WalletID)
		}
		keys[w.Address] = k
	}
	return keys, nil
}
