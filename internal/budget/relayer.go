package budget

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"

	_ "modernc.org/sqlite"
)

// Config is one deployment of the budget contract and the prover.
type Config struct {
	ChainID       uint64
	Contract      eth.Address
	Confirmations uint64
	PolicyTTL     uint64 // seconds a policy signature stays valid (the contract's deadline)
	Zkbudget      string // the zkbudget binary
	SetupDir      string // its pk.bin and vk.bin (the deployed verifier's)
}

// Keys are the relayer's three test keys (P2-D2): gas sends, the issuer
// registers tasks, the policy signer authorizes each spend.
type Keys struct{ Gas, Issuer, Policy *eth.Key }

// Relayer holds each task's private budget state (balance and r, the
// witness) in its own database and reserves orders against it.
type Relayer struct {
	cfg   Config
	keys  Keys
	rpc   *eth.Client
	db    *sql.DB
	clock func() uint64
	poll  time.Duration
	wait  time.Duration // the longest one call waits for a receipt
	// ponytail: one lock for every task and each key's nonce; per-task locks
	// and a nonce manager if two consumers' reservations must overlap.
	mu sync.Mutex
}

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
  task_id       TEXT PRIMARY KEY,
  consumer_id   TEXT NOT NULL,
  balance       INTEGER NOT NULL CHECK (balance >= 0),
  r             TEXT NOT NULL,
  commitment    TEXT NOT NULL,
  register_raw   TEXT NOT NULL,
  register_hash  TEXT NOT NULL UNIQUE,
  register_nonce INTEGER NOT NULL,
  state         TEXT NOT NULL CHECK (state IN ('registering', 'registered', 'failed'))
) STRICT;
CREATE TABLE IF NOT EXISTS reservations (
  intent_hash TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL REFERENCES tasks (task_id),
  amount      INTEGER NOT NULL CHECK (amount > 0),
  c_old       TEXT NOT NULL,
  c_new       TEXT NOT NULL,
  r_new       TEXT NOT NULL,
  nullifier   TEXT NOT NULL,
  raw_tx      TEXT NOT NULL,
  tx_hash     TEXT NOT NULL UNIQUE,
  nonce       INTEGER NOT NULL,
  deadline    INTEGER NOT NULL,
  state       TEXT NOT NULL CHECK (state IN ('sent', 'reserved', 'failed')),
  block       INTEGER,
  created_at  INTEGER NOT NULL
) STRICT;
CREATE UNIQUE INDEX IF NOT EXISTS one_open ON reservations (task_id) WHERE state = 'sent';
-- A reverted or dropped reservation leaves the task's state, so its
-- nullifier is proven again by the next one.
CREATE UNIQUE INDEX IF NOT EXISTS one_spend ON reservations (nullifier) WHERE state <> 'failed';
`

// Open opens (or creates, 0600) the relayer's database at path.
func Open(ctx context.Context, cfg Config, keys Keys, rpc *eth.Client, path string, clock func() uint64) (*Relayer, error) {
	if keys.Gas == nil || keys.Issuer == nil || keys.Policy == nil {
		return nil, errors.New("budget: the gas, issuer and policy keys")
	}
	if cfg.Confirmations == 0 {
		return nil, errors.New("budget: at least one confirmation")
	}
	if keys.Gas.Addr == keys.Issuer.Addr {
		return nil, errors.New("budget: the gas and issuer keys must differ (each has its own nonces)")
	}
	if f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600); err == nil {
		f.Close()
	} else if !errors.Is(err, os.ErrExist) {
		return nil, err
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s must be private to its owner (0600)", path)
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Relayer{cfg: cfg, keys: keys, rpc: rpc, db: db, clock: clock, poll: 200 * time.Millisecond, wait: 2 * time.Minute}, nil
}

func (r *Relayer) Close() error { return r.db.Close() }

// zk runs zkbudget; the witness, when there is one, goes on stdin only.
func (r *Relayer) zk(ctx context.Context, witness any, out any, args ...string) error {
	cmd := exec.CommandContext(ctx, r.cfg.Zkbudget, args...)
	cmd.Env = []string{}
	if witness != nil {
		w, err := json.Marshal(witness)
		if err != nil {
			return err
		}
		cmd.Stdin = bytes.NewReader(w)
		cmd.Args = append(cmd.Args, "-witness-stdin")
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("zkbudget %s: %v: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return json.Unmarshal(stdout.Bytes(), out)
}

func (r *Relayer) random(ctx context.Context) (string, error) {
	var v struct{ R string }
	err := r.zk(ctx, nil, &v, "random")
	return v.R, err
}

func (r *Relayer) chainArgs(taskID string) []string {
	return []string{"-chain", strconv.FormatUint(r.cfg.ChainID, 10), "-contract", r.cfg.Contract.Hex(), "-task", taskID}
}

func decimal(s string) (*big.Int, error) {
	v, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("a decimal %q", s)
	}
	return v, nil
}

// resendOpen sends every written, unfinished transaction again (the same
// ones), before a new one takes a nonce: a transaction written before a
// crash is never overtaken by a later one at its nonce.
func (r *Relayer) resendOpen(ctx context.Context) error {
	rows, err := r.db.QueryContext(ctx, `SELECT raw_tx FROM reservations WHERE state = 'sent' UNION ALL SELECT register_raw FROM tasks WHERE state = 'registering'`)
	if err != nil {
		return err
	}
	var raws []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return err
		}
		raws = append(raws, s)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, s := range raws {
		raw, _ := hex.DecodeString(s)
		r.rpc.SendRaw(ctx, raw) // known, mined or dropped: its own await says
	}
	return nil
}

// prepare signs a call from key after a simulation that must not revert
// (Z2 §10 S3 (c)): a transaction that would fail is never sent. Its nonce
// is the pending one, which must not be below floor (the key's written,
// unfinished transactions' next).
func (r *Relayer) prepare(ctx context.Context, key *eth.Key, data []byte, floor uint64) (raw, hash []byte, nonce uint64, err error) {
	msg := eth.Msg{From: key.Addr, To: r.cfg.Contract, Data: data}
	if _, err := r.rpc.Call(ctx, msg); err != nil {
		var re *eth.RPCError
		if errors.As(err, &re) {
			return nil, nil, 0, signer.Refusal("RESERVATION_SIMULATION_FAILED")
		}
		return nil, nil, 0, err
	}
	gas, err := r.rpc.EstimateGas(ctx, msg)
	if err != nil {
		return nil, nil, 0, err
	}
	nonce, err = r.rpc.PendingNonce(ctx, key.Addr)
	if err != nil {
		return nil, nil, 0, err
	}
	if nonce < floor {
		// A written transaction at or above it is in no pool (its resend was
		// refused): a new one would wait behind that gap. Retry later; it is
		// decided once its deadline or nonce says so, or signed again
		// (settleOpen).
		return nil, nil, 0, signer.Refusal("RELAYER_NONCE_BLOCKED")
	}
	raw, hash, err = r.signAt(ctx, key, data, nonce, gas+gas/5)
	return raw, hash, nonce, err
}

// signAt signs a call to the contract at nonce with the current fees.
func (r *Relayer) signAt(ctx context.Context, key *eth.Key, data []byte, nonce, gas uint64) (raw, hash []byte, err error) {
	return r.rpc.SignCall(ctx, r.cfg.ChainID, key, r.cfg.Contract, data, nonce, gas)
}

// resignRegistrations signs again, at its own nonce and with the current
// fees, every registration no pool holds (its nonce is not below the
// issuer's pending one: its resend was refused, say for a fee cap now
// under the base fee). Registrations have no deadline to drop them, so
// otherwise one would keep the issuer's nonce a gap forever. The same
// call at the same nonce: whichever version mines registers the same
// commitment, and finishRegistration decides by that commitment.
func (r *Relayer) resignRegistrations(ctx context.Context) error {
	pending, err := r.rpc.PendingNonce(ctx, r.keys.Issuer.Addr)
	if err != nil {
		return err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT task_id, commitment, register_nonce FROM tasks WHERE state = 'registering' AND register_nonce >= ?`, pending)
	if err != nil {
		return err
	}
	type reg struct {
		task, c0 string
		nonce    uint64
	}
	var regs []reg
	for rows.Next() {
		var x reg
		if err := rows.Scan(&x.task, &x.c0, &x.nonce); err != nil {
			rows.Close()
			return err
		}
		regs = append(regs, x)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	for _, x := range regs {
		c0, err := decimal(x.c0)
		if err != nil {
			return err
		}
		data, _ := eth.Call("registerTask(string,uint256)", x.task, c0)
		gas, err := r.rpc.EstimateGas(ctx, eth.Msg{From: r.keys.Issuer.Addr, To: r.cfg.Contract, Data: data})
		if err != nil {
			continue // the commitment decides it: registered meanwhile, say
		}
		raw, hash, err := r.signAt(ctx, r.keys.Issuer, data, x.nonce, gas+gas/5)
		if err != nil {
			return err
		}
		if _, err := r.db.ExecContext(ctx, `UPDATE tasks SET register_raw = ?, register_hash = ? WHERE task_id = ? AND state = 'registering'`,
			hex.EncodeToString(raw), hex.EncodeToString(hash), x.task); err != nil {
			return err
		}
		r.rpc.SendRaw(ctx, raw)
	}
	return nil
}

// registered: the contract holds commitment for taskID at confirmed depth.
func (r *Relayer) registered(ctx context.Context, taskID, commitment string) (bool, error) {
	latest, err := r.rpc.BlockNumber(ctx)
	if err != nil || latest+1 < r.cfg.Confirmations {
		return false, err
	}
	data, _ := eth.Call("commitment(uint256)", TaskField(r.cfg.ChainID, r.cfg.Contract, taskID))
	out, err := r.rpc.CallAt(ctx, eth.Msg{From: r.keys.Issuer.Addr, To: r.cfg.Contract, Data: data}, latest+1-r.cfg.Confirmations)
	if err != nil {
		return false, err
	}
	c, err := eth.Uint(out, 0)
	return err == nil && c.String() == commitment, err
}

// floor is the next nonce after key's written, unfinished transactions.
func (r *Relayer) floor(ctx context.Context, query string) (uint64, error) {
	var n sql.NullInt64
	err := r.db.QueryRowContext(ctx, query).Scan(&n)
	if !n.Valid {
		return 0, err
	}
	return uint64(n.Int64) + 1, err
}

// check looks once at a written transaction at the configured confirmed
// depth (eth.Client.Check).
func (r *Relayer) check(ctx context.Context, hash []byte, from eth.Address, nonce, deadline uint64) (*eth.Receipt, error) {
	return r.rpc.Check(ctx, hash, from, nonce, deadline, r.cfg.Confirmations)
}

// await sends raw again (the same transaction: never another) and checks
// it until it is decided, for at most r.wait (then nil, nil: not yet).
func (r *Relayer) await(ctx context.Context, raw []byte, hash []byte, from eth.Address, nonce, deadline uint64) (*eth.Receipt, error) {
	ctx, cancel := context.WithTimeout(ctx, r.wait)
	defer cancel()
	r.rpc.SendRaw(ctx, raw) // already known, or mined: the receipt says
	for {
		rc, err := r.check(ctx, hash, from, nonce, deadline)
		if rc != nil || errors.Is(err, eth.ErrDropped) {
			return rc, err
		}
		if err != nil && ctx.Err() == nil {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(r.poll):
		}
	}
}

// settleOpen sends every written, unfinished transaction again and checks
// each once without waiting: a decided one is finished (reserved, or
// failed). It runs before a new transaction is prepared, so an abandoned
// one never keeps its task busy or the nonce floor raised once decided.
func (r *Relayer) settleOpen(ctx context.Context) error {
	if err := r.resendOpen(ctx); err != nil {
		return err
	}
	if err := r.resignRegistrations(ctx); err != nil {
		return err
	}
	var intents []string
	var regs [][4]string
	rows, err := r.db.QueryContext(ctx, `SELECT intent_hash, '', '', '' FROM reservations WHERE state = 'sent'
		UNION ALL SELECT task_id, register_raw, register_hash, register_nonce FROM tasks WHERE state = 'registering'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var x [4]string
		if err := rows.Scan(&x[0], &x[1], &x[2], &x[3]); err != nil {
			rows.Close()
			return err
		}
		if x[1] == "" {
			intents = append(intents, x[0])
		} else {
			regs = append(regs, x)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	// Each row's own outcome is its own caller's: an error here (the node,
	// or a row needing an administrator) never stops an unrelated call.
	for _, i := range intents {
		r.resume(ctx, i, false)
	}
	for _, x := range regs {
		n, _ := strconv.ParseUint(x[3], 10, 64)
		r.finishRegistration(ctx, x[0], x[1], x[2], n, false)
	}
	return nil
}

// RegisterTask gives taskID a budget of balance for consumerID: a fresh r,
// its commitment registered by the issuer key. Again for the same task it
// finishes a registration left unconfirmed, and refuses another consumer
// or balance.
func (r *Relayer) RegisterTask(ctx context.Context, taskID, consumerID string, balance uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	// First: this task's own open registration may be decided, or signed again.
	if err := r.settleOpen(ctx); err != nil {
		return err
	}
	var have struct {
		consumer, raw, hash, state string
		balance, nonce             uint64
	}
	err := r.db.QueryRowContext(ctx, `SELECT consumer_id, balance, register_raw, register_hash, register_nonce, state FROM tasks WHERE task_id = ?`, taskID).
		Scan(&have.consumer, &have.balance, &have.raw, &have.hash, &have.nonce, &have.state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case have.consumer != consumerID || have.balance != balance:
		return signer.Refusal("TASK_REGISTERED")
	case have.state == "registered":
		return nil
	case have.state == "failed":
		return signer.Refusal("TASK_REGISTRATION_FAILED")
	default:
		return r.finishRegistration(ctx, taskID, have.raw, have.hash, have.nonce, true)
	}
	rv, err := r.random(ctx)
	if err != nil {
		return err
	}
	var c struct{ Commitment string }
	if err := r.zk(ctx, map[string]any{"balance": balance, "r": rv}, &c, append([]string{"commit"}, r.chainArgs(taskID)...)...); err != nil {
		return err
	}
	c0, err := decimal(c.Commitment)
	if err != nil {
		return err
	}
	data, err := eth.Call("registerTask(string,uint256)", taskID, c0)
	if err != nil {
		return err
	}
	floor, err := r.floor(ctx, `SELECT max(register_nonce) FROM tasks WHERE state = 'registering'`)
	if err != nil {
		return err
	}
	raw, hash, nonce, err := r.prepare(ctx, r.keys.Issuer, data, floor)
	if err != nil {
		return err
	}
	// Written before it is sent: a restart resends this transaction, never a new one.
	if _, err := r.db.ExecContext(ctx, `INSERT INTO tasks (task_id, consumer_id, balance, r, commitment, register_raw, register_hash, register_nonce, state)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'registering')`, taskID, consumerID, balance, rv, c0.String(), hex.EncodeToString(raw), hex.EncodeToString(hash), nonce); err != nil {
		return err
	}
	if err := at("after-register-write"); err != nil {
		return err
	}
	return r.finishRegistration(ctx, taskID, hex.EncodeToString(raw), hex.EncodeToString(hash), nonce, true)
}

// finishRegistration decides a registration: waiting, or checking once.
func (r *Relayer) finishRegistration(ctx context.Context, taskID, rawHex, hashHex string, nonce uint64, wait bool) error {
	raw, _ := hex.DecodeString(rawHex)
	hash, _ := hex.DecodeString(hashHex)
	var rc *eth.Receipt
	var err error
	if wait {
		rc, err = r.await(ctx, raw, hash, r.keys.Issuer.Addr, nonce, 0)
	} else {
		rc, err = r.check(ctx, hash, r.keys.Issuer.Addr, nonce, 0)
	}
	if err != nil && !errors.Is(err, eth.ErrDropped) {
		return err
	}
	if rc == nil && err == nil {
		return signer.Refusal("TASK_REGISTRATION_PENDING")
	}
	// Decided by the contract, not by this one transaction: a version
	// signed again at the same nonce may be the one that mined.
	var c0 string
	if err := r.db.QueryRowContext(ctx, `SELECT commitment FROM tasks WHERE task_id = ?`, taskID).Scan(&c0); err != nil {
		return err
	}
	ok, err := r.registered(ctx, taskID, c0)
	if err != nil {
		return err
	}
	state := "registered"
	if !ok {
		state = "failed"
	}
	if _, err := r.db.ExecContext(ctx, `UPDATE tasks SET state = ? WHERE task_id = ? AND state = 'registering'`, state, taskID); err != nil {
		return err
	}
	if state == "failed" {
		return signer.Refusal("TASK_REGISTRATION_FAILED")
	}
	return nil
}

// Reserve reserves the order's amount from its task's budget (tx#1) and
// returns the evidence for the Signer. The intent and the amount are the
// relayer's own reading of the whole order (Z2 §10 S3 (b)); the task must
// be the order's consumer's. Again for the same order it answers the same
// reservation, or keeps waiting for it: one intent is reserved at most
// once, and a reservation left sent is resumed with the same transaction.
// A refusal before the transaction is written leaves nothing behind.
func (r *Relayer) Reserve(ctx context.Context, order json.RawMessage) (Evidence, error) {
	var po paycontract.PurchaseOrder
	if err := contracts.Decode(order, &po); err != nil || po.Validate() != nil {
		return Evidence{}, signer.Refusal("ORDER_INVALID")
	}
	intent, err := po.IntentHash()
	if err != nil {
		return Evidence{}, signer.Refusal("ORDER_INVALID")
	}
	amount, err := po.PaymentBinding.Amount.Units()
	ih, err2 := IntentBytes(intent)
	if err != nil || err2 != nil || amount == 0 {
		return Evidence{}, signer.Refusal("ORDER_INVALID")
	}
	if po.PaymentBinding.ChainID != strconv.FormatUint(r.cfg.ChainID, 10) {
		return Evidence{}, signer.Refusal("CHAIN_MISMATCH")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev, done, err := r.resume(ctx, intent, true); done || err != nil {
		return ev, err
	}
	now := r.clock()
	if po.PaymentBinding.ValidBefore <= now {
		return Evidence{}, signer.Refusal("ORDER_EXPIRED") // the Signer would refuse it: spend nothing on it
	}
	if err := r.settleOpen(ctx); err != nil {
		return Evidence{}, err
	}
	taskID := po.Intent.TaskID
	var consumer, rv, cOldS, state string
	var balance uint64
	err = r.db.QueryRowContext(ctx, `SELECT consumer_id, balance, r, commitment, state FROM tasks WHERE task_id = ?`, taskID).
		Scan(&consumer, &balance, &rv, &cOldS, &state)
	switch {
	case errors.Is(err, sql.ErrNoRows) || err == nil && state != "registered":
		return Evidence{}, signer.Refusal("TASK_NOT_REGISTERED")
	case err != nil:
		return Evidence{}, err
	case consumer != po.Intent.ConsumerID:
		return Evidence{}, signer.Refusal("CONSUMER_MISMATCH")
	case amount > balance:
		return Evidence{}, signer.Refusal("OVER_BUDGET")
	}
	var open int
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM reservations WHERE task_id = ? AND state = 'sent'`, taskID).Scan(&open); err != nil {
		return Evidence{}, err
	} else if open > 0 {
		return Evidence{}, signer.Refusal("TASK_BUSY") // the commitment is being spent by another order
	}
	rNew, err := r.random(ctx)
	if err != nil {
		return Evidence{}, err
	}
	var p struct {
		Proof  string
		Inputs []string
	}
	args := append(append([]string{"prove", "-dir", r.cfg.SetupDir}, r.chainArgs(taskID)...), "-amount", strconv.FormatUint(amount, 10), "-intent", "0x"+hex.EncodeToString(ih[:]))
	if err := r.zk(ctx, map[string]any{"balance": balance, "r": rv, "rnew": rNew}, &p, args...); err != nil {
		return Evidence{}, err
	}
	if len(p.Inputs) != 7 {
		return Evidence{}, errors.New("zkbudget prove: 7 public inputs")
	}
	in := make([]*big.Int, 7)
	for i, s := range p.Inputs {
		if in[i], err = decimal(s); err != nil {
			return Evidence{}, err
		}
	}
	task, cOld, cNew, nullifier := in[0], in[1], in[3], in[4]
	if task.Cmp(TaskField(r.cfg.ChainID, r.cfg.Contract, taskID)) != 0 || cOld.String() != cOldS || !in[2].IsUint64() || in[2].Uint64() != amount {
		return Evidence{}, errors.New("zkbudget prove: the proof is not for this task, state or amount")
	}
	proof, err := hex.DecodeString(strings.TrimPrefix(p.Proof, "0x"))
	if err != nil {
		return Evidence{}, err
	}
	epochRaw, err := r.rpc.Call(ctx, eth.Msg{From: r.keys.Gas.Addr, To: r.cfg.Contract, Data: eth.Selector("epoch()")})
	if err != nil {
		return Evidence{}, err
	}
	epoch, err := eth.Uint(epochRaw, 0)
	if err != nil {
		return Evidence{}, err
	}
	deadline := min(now+r.cfg.PolicyTTL, po.PaymentBinding.ValidBefore)
	digest, err := PolicyDigest(r.cfg.ChainID, r.cfg.Contract, task, ih, amount, nullifier, cNew, deadline, epoch)
	if err != nil {
		return Evidence{}, err
	}
	sig, err := r.keys.Policy.SignPersonal(digest)
	if err != nil {
		return Evidence{}, err
	}
	data, err := eth.Call(spendSig, taskID, cOld, amount, cNew, nullifier, ih, deadline, proof, sig)
	if err != nil {
		return Evidence{}, err
	}
	floor, err := r.floor(ctx, `SELECT max(nonce) FROM reservations WHERE state = 'sent'`)
	if err != nil {
		return Evidence{}, err
	}
	raw, hash, nonce, err := r.prepare(ctx, r.keys.Gas, data, floor)
	if err != nil {
		return Evidence{}, err
	}
	// Written before it is sent: a restart resends this transaction, never a new one.
	if _, err := r.db.ExecContext(ctx, `INSERT INTO reservations (intent_hash, task_id, amount, c_old, c_new, r_new, nullifier, raw_tx, tx_hash, nonce, deadline, state, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'sent', ?)`, intent, taskID, amount, cOld.String(), cNew.String(), rNew, nullifier.String(),
		hex.EncodeToString(raw), hex.EncodeToString(hash), nonce, deadline, now); err != nil {
		return Evidence{}, err
	}
	if err := at("after-write"); err != nil {
		return Evidence{}, err
	}
	ev, _, err := r.resume(ctx, intent, true)
	return ev, err
}

// resume answers an intent's reservation, waiting for an open one or
// checking it once: done is false when there is none.
func (r *Relayer) resume(ctx context.Context, intent string, wait bool) (Evidence, bool, error) {
	var ev Evidence
	var rawHex, hashHex, state string
	var block sql.NullInt64
	var nonce, deadline uint64
	err := r.db.QueryRowContext(ctx, `SELECT task_id, amount, raw_tx, tx_hash, nonce, deadline, state, block FROM reservations WHERE intent_hash = ?`, intent).
		Scan(&ev.TaskID, &ev.Amount, &rawHex, &hashHex, &nonce, &deadline, &state, &block)
	if errors.Is(err, sql.ErrNoRows) {
		return ev, false, nil
	} else if err != nil {
		return ev, true, err
	}
	ev.ChainID, ev.Contract, ev.IntentHash, ev.TxHash = r.cfg.ChainID, r.cfg.Contract.Hex(), intent, "0x"+hashHex
	switch state {
	case "reserved":
		ev.BlockNumber = uint64(block.Int64)
		return ev, true, nil
	case "failed":
		return ev, true, signer.Refusal("RESERVATION_FAILED")
	}
	raw, _ := hex.DecodeString(rawHex)
	hash, _ := hex.DecodeString(hashHex)
	var rc *eth.Receipt
	if wait {
		rc, err = r.await(ctx, raw, hash, r.keys.Gas.Addr, nonce, deadline)
	} else {
		rc, err = r.check(ctx, hash, r.keys.Gas.Addr, nonce, deadline)
	}
	if err != nil && !errors.Is(err, eth.ErrDropped) {
		return ev, true, err
	}
	if rc == nil && err == nil {
		return ev, true, signer.Refusal("RESERVATION_PENDING") // unknown: never a second transaction
	}
	tx, err2 := r.db.BeginTx(ctx, nil)
	if err2 != nil {
		return ev, true, err2
	}
	defer tx.Rollback()
	if err != nil || rc.Status != 1 {
		// Reverted, or never to be mined: nothing was spent, the task's state stays.
		var at any
		if rc != nil {
			at = rc.BlockNumber
		}
		if _, err := tx.ExecContext(ctx, `UPDATE reservations SET state = 'failed', block = ? WHERE intent_hash = ? AND state = 'sent'`, at, intent); err != nil {
			return ev, true, err
		}
		return ev, true, errors.Join(tx.Commit(), signer.Refusal("RESERVATION_FAILED"))
	}
	// Spent on chain: the task's state is the new one, in the same write,
	// once only and only from the state this reservation spent.
	res, err := tx.ExecContext(ctx, `UPDATE reservations SET state = 'reserved', block = ? WHERE intent_hash = ? AND state = 'sent'`, rc.BlockNumber, intent)
	if err != nil {
		return ev, true, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ev, true, errors.New("budget: the reservation was finished meanwhile")
	}
	res, err = tx.ExecContext(ctx, `UPDATE tasks SET balance = balance - (SELECT amount FROM reservations WHERE intent_hash = ?),
		r = (SELECT r_new FROM reservations WHERE intent_hash = ?), commitment = (SELECT c_new FROM reservations WHERE intent_hash = ?)
		WHERE task_id = ? AND commitment = (SELECT c_old FROM reservations WHERE intent_hash = ?)`, intent, intent, intent, ev.TaskID, intent)
	if err != nil {
		return ev, true, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ev, true, errors.New("budget: the task's state is not the one this reservation spent")
	}
	ev.BlockNumber = rc.BlockNumber
	return ev, true, tx.Commit()
}

// hook runs at the named points (tests only; nil in production).
var hook func(stage string) error

func at(stage string) error {
	if hook == nil {
		return nil
	}
	return hook(stage)
}
