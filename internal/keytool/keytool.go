// Package keytool is the terminal side of the encrypted key files
// (internal/signer/keystore.go): the passphrase read with echo off, the
// private directory check, a new key file and opening one. apa-signer uses
// it for the payment wallets and apa-relayer for the Host's own keys
// (docs/commercial-p2b.md B1), each under its own label.
package keytool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"golang.org/x/sys/unix"
)

// The Host's key labels (docs/commercial-p2b.md P2b-D3, D4): apa-relayer's
// three keys and the shop's one; the payment wallets' is signer.SignerLabel.
const (
	RelayerLabel = "apa-relayer-key/v1"
	ShopLabel    = "apa-shop-key/v1"
)

// Roles are the Host's keys by ID, each with its label.
var Roles = map[string]string{"relayer-gas": RelayerLabel, "issuer": RelayerLabel, "policy": RelayerLabel, "shop": ShopLabel}

// MinPassphrase is the shortest passphrase Keygen accepts.
const MinPassphrase = 12

// Private refuses a directory that is not this user's alone.
func Private(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || !ok || st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("%s must be a directory owned by this user, mode 0700", dir)
	}
	return nil
}

// Keygen writes one new key file DIR/keys/<id>.key under label: a fresh
// random key, encrypted with a passphrase read twice (they must match). An
// existing file is never replaced. It returns the address only.
func Keygen(dir, label, id string, passphrase func() ([]byte, error)) (string, error) {
	if err := Private(dir); err != nil {
		return "", err
	}
	keys := filepath.Join(dir, "keys")
	if err := Private(keys); err != nil {
		return "", err
	}
	if !signer.ValidWalletID(id) {
		return "", fmt.Errorf("key ID %q is not valid", id)
	}
	path := filepath.Join(keys, id+".key")
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%s exists or cannot be checked; nothing was written", path)
	}
	pass, err := passphrase()
	if err != nil {
		return "", err
	}
	defer clear(pass)
	again, err := passphrase()
	if err != nil {
		return "", err
	}
	defer clear(again)
	switch {
	case !bytes.Equal(pass, again):
		return "", errors.New("the passphrases differ; nothing was written")
	case len(pass) < MinPassphrase:
		return "", fmt.Errorf("the passphrase must be at least %d characters; nothing was written", MinPassphrase)
	}
	key, err := secp256k1.GeneratePrivateKey()
	if err != nil {
		return "", err
	}
	defer key.Zero()
	priv := key.Serialize()
	defer clear(priv)
	f, err := signer.EncryptKeyFor(label, id, priv, pass, signer.ScryptN)
	if err != nil {
		return "", err
	}
	if k, err := signer.DecryptKeyFor(label, f, id, pass); err != nil || signer.Address(k.PubKey()) != f.Address {
		return "", errors.New("the new key file does not decrypt to its address; nothing was written")
	}
	raw, err := json.Marshal(f)
	if err != nil {
		return "", err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		os.Remove(path)
		return "", err
	}
	if err := errors.Join(file.Sync(), file.Close()); err != nil {
		os.Remove(path)
		return "", err
	}
	return f.Address, nil
}

// Load opens DIR/keys/<id>.key under label with the passphrase.
func Load(dir, label, id string, pass []byte) (*eth.Key, error) {
	f, err := signer.ReadKeyFile(filepath.Join(dir, "keys", id+".key"))
	if err != nil {
		return nil, err
	}
	k, err := signer.DecryptKeyFor(label, f, id, pass)
	if err != nil {
		return nil, fmt.Errorf("key %s: %w", id, err)
	}
	priv := k.Serialize()
	defer clear(priv)
	k.Zero()
	return eth.NewKey(priv)
}

// ReadPassphrase reads one line from the terminal with echo off, after
// "<program> <prompt>".
func ReadPassphrase(tty *os.File, program, prompt string) ([]byte, error) {
	fmt.Fprint(tty, program+" "+prompt)
	fd := int(tty.Fd())
	old, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return nil, err
	}
	quiet := *old
	quiet.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, setTermios, &quiet); err != nil {
		return nil, err
	}
	defer func() { unix.IoctlSetTermios(fd, setTermios, old); fmt.Fprintln(tty) }()
	var line []byte
	b := make([]byte, 1)
	for len(line) < 1024 {
		if n, err := tty.Read(b); err != nil || n == 0 {
			return nil, errors.New("no passphrase")
		}
		if b[0] == '\n' || b[0] == '\r' {
			return line, nil
		}
		line = append(line, b[0])
	}
	return nil, errors.New("passphrase too long")
}
