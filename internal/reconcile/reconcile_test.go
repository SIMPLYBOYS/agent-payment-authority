package reconcile

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"strconv"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth/ethtest"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

var bg = context.Background()

type chain struct {
	t     *testing.T
	rpc   *eth.Client
	usdc  eth.Address
	payer *eth.Key
}

func newChain(t *testing.T) *chain {
	c := ethtest.Anvil(t)
	usdc := ethtest.Deploy(t, c, "TestUSDC.sol/TestUSDC")
	pk, _ := eth.NewKey(eth.Keccak256([]byte("reconcile test payer")))
	data, _ := eth.Call("mint(address,uint256)", pk.Addr, uint64(5_000_000))
	ethtest.Mine(t, c, ethtest.Key(t, 0), &eth.Tx{To: &usdc, Gas: 200_000, Data: data})
	return &chain{t: t, rpc: c, usdc: usdc, payer: pk}
}

var shopAddr, _ = eth.ParseAddress("0x3333333333333333333333333333333333333333")

// sign is the payer's EIP-3009 authorization and the transferWithAuthorization call for it.
func (c *chain) sign(to eth.Address, value uint64, window uint64) (Authorization, []byte) {
	now := ethtest.Now(c.t, c.rpc)
	a := Authorization{From: c.payer.Addr, To: to, Value: value, ValidBefore: now + window}
	rand.Read(a.Nonce[:])
	dom := signer.Domain{Name: "USDC", Version: "2", ChainID: strconv.Itoa(ethtest.Chain), VerifyingContract: c.usdc.Hex()}
	msg := signer.Transfer{From: a.From.Hex(), To: a.To.Hex(), Value: strconv.FormatUint(value, 10), ValidAfter: 0, ValidBefore: a.ValidBefore,
		Nonce: "0x" + hex.EncodeToString(a.Nonce[:])}
	sep, _ := signer.DomainSeparator(dom)
	th, _ := signer.TransferHash(msg)
	rs, rec, _ := c.payer.Sign(signer.TypedDigest(sep, th))
	var r, s eth.Bytes32
	copy(r[:], rs[:32])
	copy(s[:], rs[32:])
	data, _ := eth.Call("transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)",
		a.From, a.To, value, uint64(0), a.ValidBefore, a.Nonce, uint8(27+rec), r, s)
	return a, data
}

func (c *chain) submit(data []byte) *eth.Receipt {
	return ethtest.Mine(c.t, c.rpc, ethtest.Key(c.t, 1), &eth.Tx{To: &c.usdc, Gas: 200_000, Data: data})
}

func (c *chain) token(confirmations uint64) Token {
	return Token{RPC: c.rpc, Address: c.usdc, Confirmations: confirmations}
}

// Each state from the chain, the same again when asked again.
func TestStatus(t *testing.T) {
	c := newChain(t)
	tok := c.token(1)
	paid, data := c.sign(shopAddr, 500000, 600)
	rc := c.submit(data)
	open, _ := c.sign(shopAddr, 100000, 3600) // outlives the boundary case's time jump
	brief, _ := c.sign(shopAddr, 100000, 30)
	other, _ := eth.ParseAddress("0x9999999999999999999999999999999999999999")
	misrecorded := paid
	misrecorded.To = other // the Host recorded another recipient than was paid
	misvalued := paid
	misvalued.Value = 499999

	for name, cs := range map[string]struct {
		a    Authorization
		want string
	}{
		"settled":     {paid, "settled"},
		"unused":      {open, "pending"},
		"misrecorded": {misrecorded, "needs_review"},
		"misvalued":   {misvalued, "needs_review"},
	} {
		for range 2 { // asked twice: the same answer
			r, err := tok.Status(bg, cs.a)
			if err != nil || r.State != cs.want {
				t.Fatalf("%s: %+v %v", name, r, err)
			}
			if cs.want == "settled" && (r.Block != rc.BlockNumber || r.Tx == [32]byte{}) {
				t.Fatalf("settled at %+v, mined in %d", r, rc.BlockNumber)
			}
		}
	}
	// Not yet confirmed: still pending at confirmed depth.
	late, data := c.sign(shopAddr, 100000, 600)
	c.submit(data)
	if r, _ := c.token(2).Status(bg, late); r.State != "pending" {
		t.Fatalf("one block, two confirmations: %+v", r)
	}
	if r, _ := c.token(1).Status(bg, late); r.State != "settled" {
		t.Fatalf("one confirmation: %+v", r)
	}
	// At exactly its validBefore unused: the token refuses it from then on.
	edge, _ := c.sign(shopAddr, 100000, 600)
	ethtest.RPC(t, c.rpc, "evm_setNextBlockTimestamp", edge.ValidBefore-1)
	ethtest.RPC(t, c.rpc, "evm_mine")
	if r, _ := tok.Status(bg, edge); r.State != "pending" {
		t.Fatalf("one second before: %+v", r)
	}
	ethtest.RPC(t, c.rpc, "evm_setNextBlockTimestamp", edge.ValidBefore)
	ethtest.RPC(t, c.rpc, "evm_mine")
	if ethtest.Now(t, c.rpc) != edge.ValidBefore {
		t.Fatalf("block time %d, validBefore %d", ethtest.Now(t, c.rpc), edge.ValidBefore)
	}
	if r, _ := tok.Status(bg, edge); r.State != "not_settled_expired" {
		t.Fatalf("at validBefore: %+v", r)
	}
	// Past its validBefore unused: it can never be settled.
	ethtest.RPC(t, c.rpc, "evm_increaseTime", 60)
	ethtest.RPC(t, c.rpc, "evm_mine")
	if r, _ := tok.Status(bg, brief); r.State != "not_settled_expired" {
		t.Fatalf("expired: %+v", r)
	}
	if r, _ := tok.Status(bg, open); r.State != "pending" {
		t.Fatalf("still open: %+v", r)
	}
}

// Every spend out of the payer is accounted for by an authorization the
// Signer signed, or listed: a plain transfer, an authorization not signed
// by the Signer, one paid to another recipient than signed.
func TestUnauthorized(t *testing.T) {
	c := newChain(t)
	tok := c.token(1)
	a1, d1 := c.sign(shopAddr, 500000, 600)
	c.submit(d1)
	a2, d2 := c.sign(shopAddr, 300000, 600)
	c.submit(d2)
	if spends, err := tok.Unauthorized(bg, []eth.Address{c.payer.Addr}, []Authorization{a1, a2}); err != nil || len(spends) != 0 {
		t.Fatalf("all signed: %+v %v", spends, err)
	}
	// Asked again: still none.
	if spends, _ := tok.Unauthorized(bg, []eth.Address{c.payer.Addr}, []Authorization{a1, a2}); len(spends) != 0 {
		t.Fatalf("again: %+v", spends)
	}
	// An authorization the Signer did not sign (here: not in known), and a plain transfer by the wallet's key.
	_, d3 := c.sign(shopAddr, 200000, 600)
	c.submit(d3)
	ethtest.RPC(t, c.rpc, "anvil_setBalance", c.payer.Addr.Hex(), "0xde0b6b3a7640000")
	other, _ := eth.ParseAddress("0x9999999999999999999999999999999999999999")
	plain, _ := eth.Call("transfer(address,uint256)", other, uint64(1))
	ethtest.Mine(t, c.rpc, c.payer, &eth.Tx{To: &c.usdc, Gas: 100_000, Data: plain})
	spends, err := tok.Unauthorized(bg, []eth.Address{c.payer.Addr}, []Authorization{a1, a2})
	if err != nil || len(spends) != 2 || spends[0].Value.Cmp(big.NewInt(200000)) != 0 || spends[0].Nonce == nil ||
		spends[1].To != other || spends[1].Nonce != nil {
		t.Fatalf("two unauthorized: %+v %v", spends, err)
	}
	// Unconfirmed spends are left out; so is anything before FromBlock.
	_, d4 := c.sign(shopAddr, 100000, 600)
	rc4 := c.submit(d4)
	if spends, _ := c.token(2).Unauthorized(bg, []eth.Address{c.payer.Addr}, []Authorization{a1, a2}); len(spends) != 2 {
		t.Fatalf("two confirmations: %+v", spends)
	}
	late := c.token(1)
	late.FromBlock = rc4.BlockNumber
	if spends, _ := late.Unauthorized(bg, []eth.Address{c.payer.Addr, c.payer.Addr}, nil); len(spends) != 1 || spends[0].Block != rc4.BlockNumber {
		t.Fatalf("from block %d, the wallet twice: %+v", rc4.BlockNumber, spends)
	}
	// Signed for another amount than was paid: listed.
	wrong := a2
	wrong.Value = 300001
	if spends, _ := tok.Unauthorized(bg, []eth.Address{c.payer.Addr}, []Authorization{a1, wrong}); len(spends) != 4 {
		t.Fatalf("amount differs from the signed one: %+v", spends)
	}
}

func TestStatusRefusesAPartialRecord(t *testing.T) {
	tok := Token{}
	for name, a := range map[string]Authorization{
		"no validBefore": {From: eth.Address{1}, Nonce: eth.Bytes32{1}},
		"no payer":       {ValidBefore: 1, Nonce: eth.Bytes32{1}},
		"no nonce":       {From: eth.Address{1}, ValidBefore: 1},
	} {
		if _, err := tok.Status(bg, a); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
