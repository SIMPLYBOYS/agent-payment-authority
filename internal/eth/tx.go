package eth

import (
	"math/big"
)

// rlp encodes byte strings ([]byte) and lists ([]any of them) only.
func rlp(v any) []byte {
	switch x := v.(type) {
	case []byte:
		if len(x) == 1 && x[0] < 0x80 {
			return x
		}
		return append(rlpHeader(0x80, len(x)), x...)
	case []any:
		var body []byte
		for _, e := range x {
			body = append(body, rlp(e)...)
		}
		return append(rlpHeader(0xc0, len(body)), body...)
	}
	panic("rlp: only []byte and []any")
}

func rlpHeader(base byte, n int) []byte {
	if n < 56 {
		return []byte{base + byte(n)}
	}
	l := big.NewInt(int64(n)).Bytes()
	return append([]byte{base + 55 + byte(len(l))}, l...)
}

// uintBytes is an unsigned integer's minimal big-endian bytes (0 is empty).
func uintBytes(x *big.Int) []byte { return x.Bytes() }

// Tx is an EIP-1559 (type 2) transaction with an empty access list.
type Tx struct {
	ChainID              *big.Int
	Nonce                uint64
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	Gas                  uint64
	To                   *Address // nil: a contract creation
	Value                *big.Int
	Data                 []byte
}

func (t *Tx) fields() []any {
	to := []byte{}
	if t.To != nil {
		to = t.To[:]
	}
	value := t.Value
	if value == nil {
		value = new(big.Int)
	}
	return []any{uintBytes(t.ChainID), uintBytes(new(big.Int).SetUint64(t.Nonce)), uintBytes(t.MaxPriorityFeePerGas),
		uintBytes(t.MaxFeePerGas), uintBytes(new(big.Int).SetUint64(t.Gas)), to, uintBytes(value), t.Data, []any{}}
}

// SigningHash is keccak256(0x02 ‖ rlp(fields)).
func (t *Tx) SigningHash() []byte {
	return Keccak256(append([]byte{2}, rlp(t.fields())...))
}

// Sign returns the raw signed transaction (for eth_sendRawTransaction) and
// its hash.
func (t *Tx) Sign(k *Key) (raw []byte, hash []byte, err error) {
	rs, rec, err := k.Sign(t.SigningHash())
	if err != nil {
		return nil, nil, err
	}
	f := append(t.fields(), uintBytes(big.NewInt(int64(rec))), uintBytes(new(big.Int).SetBytes(rs[:32])), uintBytes(new(big.Int).SetBytes(rs[32:])))
	raw = append([]byte{2}, rlp(f)...)
	return raw, Keccak256(raw), nil
}
