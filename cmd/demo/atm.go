package main

// The street's ATM (docs/commercial-atm.md, Aaron 11/9: "應該可以設計一個場域是
// 可以與 faucet 互動去領取 usdc 的"): the walked customer at it, the page asks
// POST /api/host/atm {request_id, consumer_id} for the customer's wallet and
// its test USDC. On the local chain the ATM mints some there and then, as
// TestUSDC's owner (anvil key 0: p2-local deploys it from that key). On the
// testnet it only reads: Circle's faucet asks a person, so Aaron claims there
// and the page asks again until the balance rises. Not a payment: no Policy,
// no task; a mint is a transfer into the wallet, which the reconciliation and
// E8 (transfers out) do not count.

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// atmMint is what one visit mints on the local chain: 10 test USDC; atmDaily
// the most a customer takes a UTC day (Aaron 11/9: 每位客人每天最多領 20), then
// the ATM says so and mints nothing until the next. The testnet's is Circle's.
const atmMint, atmDaily = 10_000_000, 20_000_000

type atm struct {
	node    *eth.Client
	chainID uint64
	token   eth.Address
	owner   *eth.Key               // TestUSDC's owner, local only; nil on the testnet: it reads
	wallets map[string]eth.Address // consumer -> its wallet (the registry's)
	out     io.Writer
	clock   store.Clock               // unix seconds: the UTC day the daily limit counts
	mu      sync.Mutex                // ponytail: one visit at a time (a mint waits for its receipt); one ATM, a few visits
	done    map[string]map[string]any // request ID -> its mint's reply: a resend mints once
	day     uint64                    // the UTC day taken counts
	taken   map[string]uint64         // consumer -> minted that day; ponytail: in memory, a restart starts the day again
}

// atmVisit is POST /api/host/atm. Its reply: the wallet's address, its
// balance (base units) after any mint, and mode "mint" (with minted and tx)
// or "watch", or "limit" (with limit: the customer took the day's most). 404
// no_atm on a host that does not pay.
func (a *api) atmVisit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID  string `json:"request_id"`
		ConsumerID string `json:"consumer_id"`
	}
	if !postGuard(w, r, a, &req) {
		return
	}
	m := a.atm
	if m == nil {
		fail(w, http.StatusNotFound, "no_atm")
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || !scenarioID.MatchString(req.ConsumerID) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	code, v := m.visit(r.Context(), req.RequestID, req.ConsumerID)
	reply(w, code, v)
}

// visit is one request's answer: 200 with the wallet, or an error.
func (m *atm) visit(ctx context.Context, requestID, consumer string) (int, map[string]any) {
	wallet, ok := m.wallets[consumer]
	if !ok {
		return http.StatusNotFound, map[string]any{"error": "unknown_consumer"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.done[requestID]; ok {
		return http.StatusOK, v
	}
	v := map[string]any{"consumer_id": consumer, "address": wallet.Hex(), "mode": "watch"}
	if day := m.clock() / 86400; day != m.day {
		m.day, m.taken = day, map[string]uint64{}
	}
	if m.owner != nil && m.taken[consumer]+atmMint > atmDaily {
		v["mode"], v["limit"] = "limit", fmt.Sprint(atmDaily)
	} else if m.owner != nil {
		tx, err := m.mint(ctx, wallet)
		if err != nil {
			fmt.Fprintf(m.out, "demo: atm: the mint to %s failed: %v\n", consumer, err)
			return http.StatusServiceUnavailable, map[string]any{"error": "mint_failed"}
		}
		fmt.Fprintf(m.out, "demo: atm: minted %d to %s (%s) in %s\n", atmMint, consumer, wallet.Hex(), tx)
		m.taken[consumer] += atmMint
		v["mode"], v["minted"], v["tx"] = "mint", fmt.Sprint(atmMint), tx
	}
	bal, err := m.balance(ctx, wallet)
	if err != nil {
		return http.StatusServiceUnavailable, map[string]any{"error": "chain_unavailable"}
	}
	v["balance"] = bal.String()
	if v["mode"] == "mint" {
		m.done[requestID] = v
	}
	return http.StatusOK, v
}

// balance is the wallet's token balance at latest.
func (m *atm) balance(ctx context.Context, wallet eth.Address) (*big.Int, error) {
	data, _ := eth.Call("balanceOf(address)", wallet)
	out, err := m.node.Call(ctx, eth.Msg{To: m.token, Data: data})
	if err != nil || len(out) != 32 {
		return nil, fmt.Errorf("balanceOf: %v", err)
	}
	return new(big.Int).SetBytes(out), nil
}

// mint sends the owner's mint of atmMint to wallet and waits for its success.
func (m *atm) mint(ctx context.Context, wallet eth.Address) (string, error) {
	data, _ := eth.Call("mint(address,uint256)", wallet, uint64(atmMint))
	nonce, err := m.node.PendingNonce(ctx, m.owner.Addr)
	if err != nil {
		return "", err
	}
	raw, hash, _, err := m.node.SignCall(ctx, m.chainID, m.owner, m.token, data, nonce, 200_000, nil)
	if err != nil {
		return "", err
	}
	if _, err := m.node.SendRaw(ctx, raw); err != nil {
		return "", err
	}
	tx := "0x" + hex.EncodeToString(hash)
	for range 100 {
		if rc, _ := m.node.Receipt(ctx, hash); rc != nil {
			if rc.Status != 1 {
				return "", errors.New(tx + " reverted (is anvil key 0 TestUSDC's owner?)")
			}
			return tx, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", errors.New(tx + " not mined in 10 s")
}
