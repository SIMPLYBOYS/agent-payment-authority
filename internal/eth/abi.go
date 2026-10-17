package eth

import (
	"errors"
	"fmt"
	"math/big"
)

// Selector is a function's 4-byte selector, from its canonical signature
// such as "transfer(address,uint256)".
func Selector(signature string) []byte { return Keccak256([]byte(signature))[:4] }

// Topic is an event's topic 0, from its canonical signature.
func Topic(signature string) [32]byte {
	var t [32]byte
	copy(t[:], Keccak256([]byte(signature)))
	return t
}

// Bytes32 is a 32-byte value (bytes32).
type Bytes32 [32]byte

// Call encodes a function call: the selector, then each argument. The Go
// type picks the ABI type: *big.Int or uint64 (uint256), Address
// (address), Bytes32 (bytes32), uint8 (uint8), []byte (bytes), string
// (string).
func Call(signature string, args ...any) ([]byte, error) {
	enc, err := Encode(args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", signature, err)
	}
	return append(Selector(signature), enc...), nil
}

// Encode is the ABI encoding of a tuple of args (see Call).
func Encode(args ...any) ([]byte, error) {
	head := make([]byte, 0, 32*len(args))
	var tail []byte
	for _, a := range args {
		switch v := a.(type) {
		case *big.Int:
			w, err := word(v)
			if err != nil {
				return nil, err
			}
			head = append(head, w...)
		case uint64:
			w, _ := word(new(big.Int).SetUint64(v))
			head = append(head, w...)
		case uint8:
			w, _ := word(big.NewInt(int64(v)))
			head = append(head, w...)
		case Address:
			head = append(head, make([]byte, 12)...)
			head = append(head, v[:]...)
		case Bytes32:
			head = append(head, v[:]...)
		case []byte, string:
			b, ok := a.([]byte)
			if !ok {
				b = []byte(a.(string))
			}
			off, _ := word(big.NewInt(int64(32*len(args) + len(tail))))
			head = append(head, off...)
			n, _ := word(big.NewInt(int64(len(b))))
			tail = append(tail, n...)
			tail = append(tail, b...)
			if r := len(b) % 32; r != 0 {
				tail = append(tail, make([]byte, 32-r)...)
			}
		default:
			return nil, fmt.Errorf("abi: unsupported argument type %T", a)
		}
	}
	return append(head, tail...), nil
}

func word(v *big.Int) ([]byte, error) {
	if v == nil || v.Sign() < 0 || v.BitLen() > 256 {
		return nil, errors.New("abi: a uint256 is 0 to 2^256-1")
	}
	return v.FillBytes(make([]byte, 32)), nil
}

// Word is the i-th 32-byte word of ABI data (an error when short).
func Word(data []byte, i int) ([]byte, error) {
	if i < 0 || len(data) < 32*(i+1) {
		return nil, errors.New("abi: data too short")
	}
	return data[32*i : 32*(i+1)], nil
}

// Uint is the i-th word as an unsigned integer.
func Uint(data []byte, i int) (*big.Int, error) {
	w, err := Word(data, i)
	if err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(w), nil
}

// WordAddress is an address in a 32-byte word (its high 12 bytes must be 0).
func WordAddress(w []byte) (Address, error) {
	var a Address
	if len(w) != 32 {
		return a, errors.New("abi: an address word is 32 bytes")
	}
	for _, b := range w[:12] {
		if b != 0 {
			return a, errors.New("abi: not an address")
		}
	}
	copy(a[:], w[12:])
	return a, nil
}
