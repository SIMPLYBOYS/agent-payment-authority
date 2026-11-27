package eth

import (
	"context"
	"encoding/hex"
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/anviltest"
)

// anvil's first documented test key (public; local chains only).
const anvilKey0 = "ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80"

func key0(t *testing.T) *Key {
	b, _ := hex.DecodeString(anvilKey0)
	k, err := NewKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestVectors(t *testing.T) {
	if hex.EncodeToString(Keccak256()) != "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470" {
		t.Fatal("keccak of nothing")
	}
	k := key0(t)
	if k.Addr.Hex() != "0xf39fd6e51aad88f6f4ce6ab8827279cfffb92266" {
		t.Fatalf("address %s", k.Addr.Hex())
	}
	if hex.EncodeToString(Selector("transfer(address,uint256)")) != "a9059cbb" {
		t.Fatal("selector")
	}
	// cast calldata (foundry 1.8.3): a string, uint256s, bytes32 and two bytes.
	var b32 Bytes32
	b32[31] = 0xaa
	sig65 := make([]byte, 65)
	for i := range sig65 {
		sig65[i] = 0xab
	}
	got, err := Call("spend(string,uint256,uint256,uint256,uint256,bytes32,uint256,bytes,bytes)",
		"show-a-01", uint64(1), uint64(2), uint64(3), uint64(4), b32, uint64(4102444800), []byte{0x12, 0x34}, sig65)
	want := "ea7ac7bd0000000000000000000000000000000000000000000000000000000000000120000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000003000000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000aa00000000000000000000000000000000000000000000000000000000f4865700000000000000000000000000000000000000000000000000000000000000016000000000000000000000000000000000000000000000000000000000000001a0000000000000000000000000000000000000000000000000000000000000000973686f772d612d30310000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000212340000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000041ababababababababababababababababababababababababababababababababababababababababababababababababababababababababababababababababab00000000000000000000000000000000000000000000000000000000000000"
	if err != nil || hex.EncodeToString(got) != want {
		t.Fatalf("calldata %x %v", got, err)
	}
	// cast mktx (foundry 1.8.3), the same key: a value transfer, and a call with data.
	to, _ := ParseAddress("0x70997970C51812dc3A010C7d01b50e0d17dc79C8")
	tx := &Tx{ChainID: big.NewInt(31337), Nonce: 7, MaxPriorityFeePerGas: big.NewInt(1e9), MaxFeePerGas: big.NewInt(3e9), Gas: 21000, To: &to, Value: big.NewInt(12345)}
	raw, hash, err := tx.Sign(k)
	if err != nil || hex.EncodeToString(raw) != "02f86e827a6907843b9aca0084b2d05e008252089470997970c51812dc3a010c7d01b50e0d17dc79c882303980c001a05afedb974a98f3ca164301ca677ce71e8bbdae20fe631f3524547f6bbefc5ff4a00aa6d93436989b9146a2cafbdb47bd4c13cc9d475ad2ba619f83808e3223a531" {
		t.Fatalf("value tx %x %v", raw, err)
	}
	if len(hash) != 32 {
		t.Fatal("hash")
	}
	c, _ := ParseAddress("0x5FbDB2315678afecb367f032d93F642f64180aa3")
	data, _ := Call("registerTask(string,uint256)", "show-a-01", uint64(42))
	tx2 := &Tx{ChainID: big.NewInt(84532), Nonce: 0, MaxPriorityFeePerGas: big.NewInt(1), MaxFeePerGas: big.NewInt(2), Gas: 300000, To: &c, Data: data}
	raw2, _, err := tx2.Sign(k)
	if err != nil || hex.EncodeToString(raw2) != "02f8eb83014a34800102830493e0945fbdb2315678afecb367f032d93f642f64180aa380b884eb054adb0000000000000000000000000000000000000000000000000000000000000040000000000000000000000000000000000000000000000000000000000000002a000000000000000000000000000000000000000000000000000000000000000973686f772d612d30310000000000000000000000000000000000000000000000c080a04c968ea194b6cdf9d0b70e803e6dcf7549865a6e08a2b62445a3d12220bef637a03a77a22981338a0666b8a75bc8939b7fc28487a791e9aea3b20cc905ece1d440" {
		t.Fatalf("data tx %x %v", raw2, err)
	}
}

func TestRefusals(t *testing.T) {
	for _, s := range []string{"", "0x12", "f39fd6e51aad88f6f4ce6ab8827279cfffb92266", "0xzz9fd6e51aad88f6f4ce6ab8827279cfffb92266"} {
		if _, err := ParseAddress(s); err == nil {
			t.Errorf("address %q accepted", s)
		}
	}
	if _, err := NewKey(make([]byte, 32)); err == nil {
		t.Error("a zero key")
	}
	if _, err := NewKey([]byte{1}); err == nil {
		t.Error("a short key")
	}
	if _, err := Encode(big.NewInt(-1)); err == nil {
		t.Error("a negative uint256")
	}
	if _, err := Encode(new(big.Int).Lsh(big.NewInt(1), 256)); err == nil {
		t.Error("2^256")
	}
	if _, err := Encode(3.5); err == nil {
		t.Error("a float")
	}
	if _, err := WordAddress(append([]byte{1}, make([]byte, 31)...)); err == nil {
		t.Error("an address word with high bytes")
	}
}

// anvil starts the repository's local foundry anvil on a loopback port of
// its own, or skips the test when it is not there (var/ is not in git).
func anvil(t *testing.T) *Client {
	return NewClient(anviltest.Start(t))
}

// Our own signed transaction on a local anvil: mined, the value moved, the
// same raw transaction refused again.
func TestAnvil(t *testing.T) {
	c := anvil(t)
	ctx := context.Background()
	k := key0(t)
	id, err := c.ChainID(ctx)
	if err != nil || id.Int64() != 31337 {
		t.Fatalf("chain %v %v", id, err)
	}
	to, _ := ParseAddress("0x000000000000000000000000000000000000dEaD")
	before, _ := c.Balance(ctx, to)
	nonce, err := c.PendingNonce(ctx, k.Addr)
	h, err2 := c.LatestHeader(ctx)
	tip, err3 := c.MaxPriorityFee(ctx)
	if err := errors.Join(err, err2, err3); err != nil {
		t.Fatal(err)
	}
	fee := new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), tip)
	tx := &Tx{ChainID: id, Nonce: nonce, MaxPriorityFeePerGas: tip, MaxFeePerGas: fee, Gas: 21000, To: &to, Value: big.NewInt(777)}
	raw, hash, err := tx.Sign(k)
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.SendRaw(ctx, raw)
	if err != nil || hex.EncodeToString(got) != hex.EncodeToString(hash) {
		t.Fatalf("send %x %v", got, err)
	}
	var r *Receipt
	for i := 0; i < 50 && r == nil; i++ {
		if r, err = c.Receipt(ctx, hash); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if r == nil || r.Status != 1 || r.GasUsed != 21000 {
		t.Fatalf("receipt %+v", r)
	}
	after, _ := c.Balance(ctx, to)
	if new(big.Int).Sub(after, before).Int64() != 777 {
		t.Fatalf("balance %v -> %v", before, after)
	}
	if _, err := c.SendRaw(ctx, raw); err == nil {
		t.Fatal("the same signed transaction was accepted twice")
	}
	if n, _ := c.PendingNonce(ctx, k.Addr); n != nonce+1 {
		t.Fatalf("nonce %d", n)
	}
	logs, err := c.Logs(ctx, to, Topic("Transfer(address,address,uint256)"), 0, r.BlockNumber)
	if err != nil || len(logs) != 0 {
		t.Fatalf("logs %v %v", logs, err)
	}
}

// A node that does not answer is named by the method, never by its URL:
// an API key in a URL's path or query stays out of every error.
func TestRPCErrorsHideTheURL(t *testing.T) {
	c := NewClient("http://127.0.0.1:1/v2/SECRET-KEY?key=SECRET-KEY")
	_, err := c.ChainID(context.Background())
	if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "127.0.0.1:1/") {
		t.Fatalf("%v", err)
	}
	if _, err := NewClient("http://[::1/SECRET-KEY").ChainID(context.Background()); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("a URL that does not parse: %v", err)
	}
	err = c.Raw(context.Background(), "web3_clientVersion", nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "web3_clientVersion") {
		t.Fatalf("%v", err)
	}
}
