package merchant

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"

	_ "modernc.org/sqlite"
)

// SettleConfig is the shop's own settlement (P2-D3, docs/commercial-p2.md
// 9.10): it verifies an EIP-3009 authorization itself and sends
// transferWithAuthorization (tx#2) from its gas key, on one chain and token.
type SettleConfig struct {
	ChainID       uint64
	Token         eth.Address // the asset the offers name
	TokenName     string      // its EIP-712 domain name and version
	TokenVersion  string
	PayTo         eth.Address // the offers' payTo
	Confirmations uint64      // before anything is delivered (P2-D6)
	// DailyTxCap bounds the transfers sent a UTC day (0: none, a local
	// chain), counted per transaction nonce: a stuck transfer replaced at
	// its nonce is not another one (docs/commercial-p2b.md B3, P2b-D5).
	DailyTxCap uint64
	// Poll and Wait: how often and how long one request checks its
	// transfer (0: 200 ms and 20 s, an instant local chain).
	Poll, Wait time.Duration
}

// Settler verifies and settles payments and decides when a resource may be
// delivered: only once its transfer has the configured confirmations.
type Settler struct {
	cfg   SettleConfig
	gas   *eth.Key
	rpc   *eth.Client
	db    *sql.DB
	clock func() uint64
	poll  time.Duration
	wait  time.Duration // the longest one request waits for its transfer
	// mu covers the gas key's nonce: deciding, writing and sending a transfer.
	// Waiting for its confirmations does not hold it, so settlements overlap.
	mu sync.Mutex
}

const settleSchema = `
CREATE TABLE IF NOT EXISTS settlements (
  payer        TEXT NOT NULL,
  auth_nonce   TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  resource_url TEXT NOT NULL,
  amount       INTEGER NOT NULL CHECK (amount > 0),
  valid_before INTEGER NOT NULL,
  raw_tx       TEXT NOT NULL,
  tx_hash      TEXT NOT NULL UNIQUE,
  tx_nonce     INTEGER NOT NULL,
  tip          TEXT NOT NULL,
  fee_cap      TEXT NOT NULL,
  state        TEXT NOT NULL CHECK (state IN ('sent', 'settled', 'failed')),
  block        INTEGER,
  created_at   INTEGER NOT NULL,
  PRIMARY KEY (payer, auth_nonce)
) STRICT;
`

// OpenSettler opens (or creates, 0600) the shop's settlement database.
func OpenSettler(ctx context.Context, cfg SettleConfig, gas *eth.Key, rpc *eth.Client, path string, clock func() uint64) (*Settler, error) {
	if gas == nil || cfg.Confirmations == 0 || cfg.TokenName == "" || cfg.TokenVersion == "" {
		return nil, errors.New("merchant: settlement needs a gas key, at least one confirmation and the token's EIP-712 domain")
	}
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must be private to its owner (0600)", path)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, settleSchema+refundSchema); err != nil {
		db.Close()
		return nil, err
	}
	st := &Settler{cfg: cfg, gas: gas, rpc: rpc, db: db, clock: clock, poll: 200 * time.Millisecond, wait: 20 * time.Second}
	if cfg.Poll > 0 {
		st.poll = cfg.Poll
	}
	if cfg.Wait > 0 {
		st.wait = cfg.Wait
	}
	return st, nil
}

func (s *Settler) Close() error { return s.db.Close() }

// exactPayload is the x402 exact-EVM payload (EIP-3009).
type exactPayload struct {
	Signature     string `json:"signature"`
	Authorization struct {
		From        string `json:"from"`
		To          string `json:"to"`
		Value       string `json:"value"`
		ValidAfter  string `json:"validAfter"`
		ValidBefore string `json:"validBefore"`
		Nonce       string `json:"nonce"`
	} `json:"authorization"`
}

// Outcome is what the resource answers: Delivered (200 with the content
// and PAYMENT-RESPONSE), Failed (402 with PAYMENT-RESPONSE success false),
// Invalid (400), or Pending (202: the transfer is sent and not yet
// confirmed; the same PAYMENT-SIGNATURE again asks again, and never sends
// a second transfer).
type Outcome struct {
	Kind     string // delivered, failed, invalid, pending
	Reason   string
	Response SettlementResponse
}

// SettlementResponse is the PAYMENT-RESPONSE header (x402 transports-v2
// HTTP: base64 JSON): a facilitator's SettleResponse.
type SettlementResponse = x402.SettleResponse

// HeaderPaymentResponse carries the settlement result.
const HeaderPaymentResponse = "PAYMENT-RESPONSE"

type verified struct {
	payer, to          eth.Address
	nonce              eth.Bytes32
	value              uint64
	validAfter, before uint64
	sig                []byte
}

// Pay settles one payload for offer o: verified first, the transfer sent
// only then, the content due only once it is confirmed. The same payload
// again answers the same settlement; one authorization pays once.
func (s *Settler) Pay(ctx context.Context, o offer, payload json.RawMessage) Outcome {
	return s.Settle(ctx, o.Requirement, o.url, payload)
}

// Check is Settle's checks alone, reading and writing nothing of its own
// (a facilitator's /verify, docs/commercial-facilitator.md): the payer, and
// "" or why it would not be settled. settlement_node_unavailable is the
// node, not the payment.
func (s *Settler) Check(ctx context.Context, req x402.Requirement, payload json.RawMessage) (payer, reason string) {
	v, reason := parse(payload)
	if reason != "" {
		return "", reason
	}
	return v.payer.Hex(), s.verify(ctx, req, v)
}

// Settle settles one payload against requirement req (a facilitator's
// /settle; Pay for the shop's own offers). key names what it pays for: the
// same authorization for another key is a replay.
func (s *Settler) Settle(ctx context.Context, req x402.Requirement, key string, payload json.RawMessage) Outcome {
	network := "eip155:" + strconv.FormatUint(s.cfg.ChainID, 10)
	v, reason := parse(payload)
	if reason != "" {
		return Outcome{Kind: "invalid", Reason: reason}
	}
	resp := SettlementResponse{Network: network, Payer: v.payer.Hex()}
	data, err := transferCall(v)
	if err != nil {
		return Outcome{Kind: "invalid", Reason: "invalid_payload"}
	}
	// The same authorization however it is serialized: every signed field
	// and the signature, as the transfer will carry them.
	sum := sha256.Sum256(data)
	nonceHex := "0x" + hex.EncodeToString(v.nonce[:])
	// The lock covers deciding and writing a transfer; waiting for its
	// confirmations does not hold it (decide's writes are guarded by state).
	s.mu.Lock()
	locked := true
	defer func() {
		if locked {
			s.mu.Unlock()
		}
	}()
	unlockThen := func(f func() Outcome) Outcome {
		s.mu.Unlock()
		locked = false
		return f()
	}
	var have struct {
		payloadHash, url, state, hash string
	}
	err = s.db.QueryRowContext(ctx, `SELECT payload_hash, resource_url, state, tx_hash FROM settlements WHERE payer = ? AND auth_nonce = ?`,
		v.payer.Hex(), nonceHex).Scan(&have.payloadHash, &have.url, &have.state, &have.hash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return Outcome{Kind: "pending", Reason: "settlement_store_unavailable", Response: resp}
	case have.payloadHash != hex.EncodeToString(sum[:]) || have.url != key:
		// The same authorization in another payload or for another resource: a replay.
		resp.ErrorReason = "invalid_exact_evm_payload_authorization_nonce_used"
		return Outcome{Kind: "failed", Reason: resp.ErrorReason, Response: resp}
	default:
		return unlockThen(func() Outcome { return s.resume(ctx, v.payer.Hex(), nonceHex, resp) })
	}
	if r := s.verify(ctx, req, v); r == "settlement_node_unavailable" {
		return Outcome{Kind: "pending", Reason: r, Response: resp} // the node, not the payment: nothing written
	} else if r != "" {
		resp.ErrorReason = r
		return Outcome{Kind: "failed", Reason: r, Response: resp}
	}
	if err := s.settleOpen(ctx); err != nil {
		return Outcome{Kind: "pending", Reason: "settlement_store_unavailable", Response: resp}
	}
	if r := s.capped(ctx); r != "" {
		resp.ErrorReason = r // nothing written or sent: the authorization stays unused until it expires
		return Outcome{Kind: "failed", Reason: r, Response: resp}
	}
	raw, hash, txNonce, fees, r := s.prepare(ctx, data)
	if r != "" {
		return Outcome{Kind: "pending", Reason: r, Response: resp} // nothing written: the same payment again tries again
	}
	// Written before it is sent: a retry resends this transfer, never a new one.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO settlements (payer, auth_nonce, payload_hash, resource_url, amount, valid_before, raw_tx, tx_hash, tx_nonce, tip, fee_cap, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'sent', ?)`, v.payer.Hex(), nonceHex, hex.EncodeToString(sum[:]), key, v.value, v.before,
		hex.EncodeToString(raw), hex.EncodeToString(hash), txNonce, fees.Tip.String(), fees.Cap.String(), s.clock()); err != nil {
		return Outcome{Kind: "pending", Reason: "settlement_store_unavailable", Response: resp}
	}
	if err := at("after-write"); err != nil {
		return Outcome{Kind: "pending", Reason: "settlement_pending", Response: resp}
	}
	// Sent before the lock is let go: another payer's transfer, prepared next,
	// sees this one in the pending nonce instead of settlement_busy.
	s.rpc.SendRaw(ctx, raw)
	return unlockThen(func() Outcome { return s.resume(ctx, v.payer.Hex(), nonceHex, resp) })
}

// ParsePayment reads an exact-EVM payload as the shop does (its fields,
// strictly), judging nothing: a payer's check that its payload is one the
// shop can read.
func ParsePayment(payload json.RawMessage) error {
	if _, reason := parse(payload); reason != "" {
		return errors.New("merchant: " + reason)
	}
	return nil
}

// parse reads the exact-EVM payload's form; nothing is judged yet.
func parse(payload json.RawMessage) (verified, string) {
	var v verified
	var p exactPayload
	d := json.NewDecoder(bytes.NewReader(payload))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return v, "invalid_payload"
	}
	a := p.Authorization
	var err error
	if v.payer, err = eth.ParseAddress(a.From); err != nil {
		return v, "invalid_payload"
	}
	if v.to, err = eth.ParseAddress(a.To); err != nil {
		return v, "invalid_payload"
	}
	n, err := hex.DecodeString(strings.TrimPrefix(a.Nonce, "0x"))
	if err != nil || len(n) != 32 || !strings.HasPrefix(a.Nonce, "0x") {
		return v, "invalid_payload"
	}
	copy(v.nonce[:], n)
	if v.sig, err = hex.DecodeString(strings.TrimPrefix(p.Signature, "0x")); err != nil || len(v.sig) != 65 {
		return v, "invalid_payload"
	}
	var e1, e2, e3 error
	v.value, e1 = strconv.ParseUint(a.Value, 10, 64)
	v.validAfter, e2 = strconv.ParseUint(a.ValidAfter, 10, 64)
	v.before, e3 = strconv.ParseUint(a.ValidBefore, 10, 64)
	if errors.Join(e1, e2, e3) != nil {
		return v, "invalid_payload"
	}
	return v, ""
}

// verify is the shop's own verification (P2-D3): recipient, amount, window,
// the EIP-712 signer, the nonce unused and the balance on chain, and a
// simulation of the transfer. Any failure: nothing is sent.
func (s *Settler) verify(ctx context.Context, req x402.Requirement, v verified) string {
	now := s.clock()
	amount, err := req.Amount.Units()
	payTo, err2 := eth.ParseAddress(req.PayTo)
	switch {
	case errors.Join(err, err2) != nil:
		return "invalid_payment_requirements"
	case v.to != payTo:
		return "invalid_exact_evm_payload_recipient_mismatch"
	case v.value != amount:
		return "invalid_exact_evm_payload_authorization_value"
	case now <= v.validAfter:
		return "invalid_exact_evm_payload_authorization_valid_after"
	case now+settleMargin >= v.before || v.before > now+req.MaxTimeoutSeconds+clockSkew:
		// Too close to expiry to be mined in time, or longer than the offer
		// allows: a transfer that could sit unmined for long holds the
		// shop's gas nonce that long.
		return "invalid_exact_evm_payload_authorization_valid_before"
	}
	dom := signer.Domain{Name: s.cfg.TokenName, Version: s.cfg.TokenVersion, ChainID: strconv.FormatUint(s.cfg.ChainID, 10), VerifyingContract: s.cfg.Token.Hex()}
	msg := signer.Transfer{From: v.payer.Hex(), To: v.to.Hex(), Value: strconv.FormatUint(v.value, 10), ValidAfter: v.validAfter, ValidBefore: v.before,
		Nonce: "0x" + hex.EncodeToString(v.nonce[:])}
	sep, err1 := signer.DomainSeparator(dom)
	th, err2 := signer.TransferHash(msg)
	if errors.Join(err1, err2) != nil || v.sig[64] != 27 && v.sig[64] != 28 || new(big.Int).SetBytes(v.sig[32:64]).Cmp(halfN) > 0 {
		return "invalid_exact_evm_payload_signature" // low S only: one signature per authorization
	}
	pub, _, err := ecdsa.RecoverCompact(append([]byte{v.sig[64]}, v.sig[:64]...), signer.TypedDigest(sep, th))
	if err != nil || signer.Address(pub) != v.payer.Hex() {
		return "invalid_exact_evm_payload_signature"
	}
	used, err := s.rpc.Call(ctx, eth.Msg{From: s.gas.Addr, To: s.cfg.Token, Data: mustCall("authorizationState(address,bytes32)", v.payer, v.nonce)})
	if err != nil {
		return "settlement_node_unavailable"
	}
	if u, err := eth.Uint(used, 0); err != nil || u.Sign() != 0 {
		return "invalid_exact_evm_payload_authorization_nonce_used"
	}
	bal, err := s.rpc.Call(ctx, eth.Msg{From: s.gas.Addr, To: s.cfg.Token, Data: mustCall("balanceOf(address)", v.payer)})
	if err != nil {
		return "settlement_node_unavailable"
	}
	if b, err := eth.Uint(bal, 0); err != nil || b.Cmp(new(big.Int).SetUint64(v.value)) < 0 {
		return "insufficient_funds"
	}
	data, err := transferCall(v)
	if err != nil {
		return "invalid_payload"
	}
	if _, err := s.rpc.Call(ctx, eth.Msg{From: s.gas.Addr, To: s.cfg.Token, Data: data}); err != nil {
		var re *eth.RPCError
		if errors.As(err, &re) {
			return "invalid_transaction_state" // the token itself refuses it
		}
		return "settlement_node_unavailable"
	}
	return ""
}

const (
	settleMargin = 5 // seconds an authorization must still have to be sent
	clockSkew    = 5 // seconds past maxTimeoutSeconds tolerated
)

// halfN is secp256k1's group order / 2: a higher S is the malleated twin.
var halfN, _ = new(big.Int).SetString("7fffffffffffffffffffffffffffffff5d576e7357a4501ddfe92f46681b20a0", 16)

func mustCall(sig string, args ...any) []byte {
	b, err := eth.Call(sig, args...)
	if err != nil {
		panic(err) // fixed signatures and typed arguments only
	}
	return b
}

// transferCall is transferWithAuthorization(from, to, value, validAfter,
// validBefore, nonce, v, r, s) for the verified authorization.
func transferCall(v verified) ([]byte, error) {
	var r, ss eth.Bytes32
	copy(r[:], v.sig[:32])
	copy(ss[:], v.sig[32:64])
	return eth.Call("transferWithAuthorization(address,address,uint256,uint256,uint256,bytes32,uint8,bytes32,bytes32)",
		v.payer, v.to, v.value, v.validAfter, v.before, v.nonce, v.sig[64], r, ss)
}

// prepare signs the transfer from the gas key at its pending nonce. The
// nonce must not be below an unfinished settlement's next: one in no pool
// would leave a gap the new transfer waits behind (then settlement_busy,
// nothing signed; settleOpen decides it by its deadline).
func (s *Settler) prepare(ctx context.Context, data []byte) (raw, hash []byte, nonce uint64, fees eth.Fees, reason string) {
	unavailable := func() ([]byte, []byte, uint64, eth.Fees, string) {
		return nil, nil, 0, fees, "settlement_node_unavailable"
	}
	msg := eth.Msg{From: s.gas.Addr, To: s.cfg.Token, Data: data}
	gas, err := s.rpc.EstimateGas(ctx, msg)
	if err != nil {
		return unavailable()
	}
	if nonce, err = s.rpc.PendingNonce(ctx, s.gas.Addr); err != nil {
		return unavailable()
	}
	latest, err := s.rpc.BlockNumber(ctx)
	if err != nil {
		return unavailable()
	}
	mined, err := s.rpc.NonceAt(ctx, s.gas.Addr, latest)
	if err != nil {
		return unavailable()
	}
	var floor, stale sql.NullInt64
	// A refund (refund.go) takes the gas key's nonces as a settlement does: its unfinished ones count, and a
	// failed settlement's nonce a refund took is not taken again.
	if err := s.db.QueryRowContext(ctx, `SELECT (SELECT max(n) + 1 FROM (SELECT tx_nonce AS n FROM settlements WHERE state = 'sent' UNION ALL SELECT tx_nonce FROM refunds WHERE state = 'sent')),
		(SELECT min(tx_nonce) FROM settlements f WHERE state = 'failed' AND tx_nonce >= ?
			AND NOT EXISTS (SELECT 1 FROM settlements o WHERE o.state <> 'failed' AND o.tx_nonce = f.tx_nonce)
			AND NOT EXISTS (SELECT 1 FROM refunds r WHERE r.state <> 'failed' AND r.tx_nonce = f.tx_nonce))`, mined).Scan(&floor, &stale); err != nil {
		return nil, nil, 0, fees, "settlement_store_unavailable"
	}
	var replace *eth.Fees
	switch {
	case stale.Valid && uint64(stale.Int64) <= nonce:
		// A failed transfer (past its validBefore) still holds this nonce,
		// unmined (in a pool, or evicted and a gap): this one takes it,
		// priced above every transfer written at it, so a pool replaces the
		// stuck one and later transfers never wait behind it.
		nonce = uint64(stale.Int64)
		replace = &eth.Fees{Tip: new(big.Int), Cap: new(big.Int)}
		rows, err := s.db.QueryContext(ctx, `SELECT tip, fee_cap FROM settlements WHERE tx_nonce = ?`, nonce)
		if err != nil {
			return nil, nil, 0, fees, "settlement_store_unavailable"
		}
		for rows.Next() {
			var tip, fc string
			rows.Scan(&tip, &fc)
			if t, ok := new(big.Int).SetString(tip, 10); ok && t.Cmp(replace.Tip) > 0 {
				replace.Tip = t
			}
			if c, ok := new(big.Int).SetString(fc, 10); ok && c.Cmp(replace.Cap) > 0 {
				replace.Cap = c
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			return nil, nil, 0, fees, "settlement_store_unavailable"
		}
	case floor.Valid && nonce < uint64(floor.Int64):
		return nil, nil, 0, fees, "settlement_busy"
	}
	raw, hash, fees, err = s.rpc.SignCall(ctx, s.cfg.ChainID, s.gas, s.cfg.Token, data, nonce, gas+gas/5, replace)
	if err != nil {
		return unavailable()
	}
	return raw, hash, nonce, fees, ""
}

// capped refuses a new transfer once today's are DailyTxCap: the nonces
// first used today.
func (s *Settler) capped(ctx context.Context) string {
	if s.cfg.DailyTxCap == 0 {
		return ""
	}
	n, err := s.SentToday(ctx)
	if err != nil {
		return "settlement_store_unavailable"
	}
	if n >= s.cfg.DailyTxCap {
		return "settlement_daily_cap"
	}
	return ""
}

// SentToday is this UTC day's transfers as DailyTxCap counts them (the
// nonces first used today), and DailyTxCap itself (0: none).
func (s *Settler) SentToday(ctx context.Context) (uint64, error) {
	var n uint64
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT tx_nonce FROM settlements GROUP BY tx_nonce HAVING min(created_at) / 86400 = ?)`,
		s.clock()/86400).Scan(&n)
	return n, err
}

// DailyTxCap is the configured cap on transfers a UTC day (0: none).
func (s *Settler) DailyTxCap() uint64 { return s.cfg.DailyTxCap }

// Quota is the transfers sent today and the daily cap (Facilitated's, for the same question).
func (s *Settler) Quota(ctx context.Context) (sent, cap uint64, err error) {
	n, err := s.SentToday(ctx)
	return n, s.cfg.DailyTxCap, err
}

// settleOpen sends every unfinished transfer again (the same ones) and
// decides each it can without waiting, before a new one takes a nonce.
func (s *Settler) settleOpen(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT payer, auth_nonce, raw_tx FROM settlements WHERE state = 'sent'`)
	if err != nil {
		return err
	}
	var open [][3]string
	for rows.Next() {
		var x [3]string
		if err := rows.Scan(&x[0], &x[1], &x[2]); err != nil {
			rows.Close()
			return err
		}
		open = append(open, x)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, x := range open {
		raw, _ := hex.DecodeString(x[2])
		s.rpc.SendRaw(ctx, raw) // known, mined or dropped: its check says
		s.decide(ctx, x[0], x[1], false)
	}
	// Unfinished refunds the same way: sent again, decided when they can be without waiting.
	refunds, err := s.db.QueryContext(ctx, `SELECT settle_tx, raw_tx FROM refunds WHERE state = 'sent'`)
	if err != nil {
		return err
	}
	var sent [][2]string
	for refunds.Next() {
		var x [2]string
		if err := refunds.Scan(&x[0], &x[1]); err != nil {
			refunds.Close()
			return err
		}
		sent = append(sent, x)
	}
	if err := errors.Join(refunds.Err(), refunds.Close()); err != nil {
		return err
	}
	for _, x := range sent {
		raw, _ := hex.DecodeString(x[1])
		s.rpc.SendRaw(ctx, raw)
		s.refunding(ctx, x[0], false)
	}
	return nil
}

// resume answers a written settlement, waiting at most s.wait.
func (s *Settler) resume(ctx context.Context, payer, nonce string, resp SettlementResponse) Outcome {
	state, hash, err := s.decide(ctx, payer, nonce, true)
	resp.Transaction = "0x" + hash
	switch {
	case err != nil:
		return Outcome{Kind: "pending", Reason: "settlement_pending", Response: resp}
	case state == "settled":
		resp.Success = true
		return Outcome{Kind: "delivered", Response: resp}
	case state == "failed":
		resp.ErrorReason = "settlement_failed"
		return Outcome{Kind: "failed", Reason: resp.ErrorReason, Response: resp}
	}
	return Outcome{Kind: "pending", Reason: "settlement_pending", Response: resp}
}

// decide brings a settlement's state up to the chain at confirmed depth:
// settled once its transfer is confirmed and the token's Transfer of the
// amount to payTo is in it; failed when it reverted or can never be mined
// (the authorization's validBefore passed, or the gas nonce went to
// another). wait: send again and keep checking for at most s.wait.
func (s *Settler) decide(ctx context.Context, payer, nonce string, wait bool) (state, hash string, err error) {
	var rawHex string
	var txNonce, before, amount uint64
	if err := s.db.QueryRowContext(ctx, `SELECT state, tx_hash, raw_tx, tx_nonce, valid_before, amount FROM settlements WHERE payer = ? AND auth_nonce = ?`,
		payer, nonce).Scan(&state, &hash, &rawHex, &txNonce, &before, &amount); err != nil {
		return "", "", err
	}
	if state != "sent" {
		return state, hash, nil
	}
	raw, _ := hex.DecodeString(rawHex)
	h, _ := hex.DecodeString(hash)
	ctx, cancel := context.WithTimeout(ctx, s.wait)
	defer cancel()
	if wait {
		s.rpc.SendRaw(ctx, raw)
	}
	for {
		// The token refuses it at validBefore: a block after validBefore-1 can never take it.
		rc, err := s.rpc.Check(ctx, h, s.gas.Addr, txNonce, before-1, s.cfg.Confirmations)
		switch {
		case errors.Is(err, eth.ErrDropped) || rc != nil && rc.Status != 1:
			state = "failed"
		case rc != nil && !s.transferred(rc, payer, amount):
			state = "failed" // succeeded without the transfer: not a payment
		case rc != nil:
			state = "settled"
		case err != nil && ctx.Err() == nil:
			return "sent", hash, err
		}
		if state != "sent" {
			var block any
			if rc != nil {
				block = rc.BlockNumber
			}
			_, err := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE settlements SET state = ?, block = ? WHERE payer = ? AND auth_nonce = ? AND state = 'sent'`,
				state, block, payer, nonce)
			return state, hash, err
		}
		if !wait {
			return state, hash, nil
		}
		select {
		case <-ctx.Done():
			return state, hash, nil
		case <-time.After(s.poll):
		}
	}
}

var transferTopic = eth.Topic("Transfer(address,address,uint256)")

// transferred: the receipt holds the token's own Transfer of amount from
// payer to payTo.
func (s *Settler) transferred(rc *eth.Receipt, payer string, amount uint64) bool {
	from, err := eth.ParseAddress(payer)
	if err != nil {
		return false
	}
	for _, l := range rc.Logs {
		if l.Removed || l.Address != s.cfg.Token || len(l.Topics) != 3 || l.Topics[0] != transferTopic {
			continue
		}
		f, err1 := eth.WordAddress(l.Topics[1][:])
		t, err2 := eth.WordAddress(l.Topics[2][:])
		v, err3 := eth.Uint(l.Data, 0)
		if errors.Join(err1, err2, err3) == nil && f == from && t == s.cfg.PayTo && v.IsUint64() && v.Uint64() == amount {
			return true
		}
	}
	return false
}

// hook runs at the named points (tests only; nil in production).
var hook func(stage string) error

func at(stage string) error {
	if hook == nil {
		return nil
	}
	return hook(stage)
}
