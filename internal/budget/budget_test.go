package budget

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/eth"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
)

// anvil's first documented test keys (public; local chains only): the
// deployer, the issuer, the policy signer, the gas sender.
var anvilKeys = []string{
	"ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80",
	"59c6995e998f97a5a0044966f0945389dc9e86dae88c7a8412f4603b6b78690d",
	"5de4111afa1a4b94908f83103eb1f1706367c2e68ca870fc3fb9f804cdab365a",
	"7c852118294e51e653712a81e05800f419141751be58f605c371e15141b007a6",
}

var bg = context.Background()

func key(t *testing.T, i int) *eth.Key {
	b, _ := hex.DecodeString(anvilKeys[i])
	k, err := eth.NewKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func root() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "../..")
}

// world is a local anvil (chain 84532, as the test orders name) with the
// Z2 verifier and budget contract deployed, and a built zkbudget.
type world struct {
	rpc      *eth.Client
	contract eth.Address
	zkb      string
	now      uint64
}

func newWorld(t *testing.T) *world {
	t.Helper()
	bin := filepath.Join(root(), "var/tools/foundry-v1.8.3/bin/anvil")
	out := filepath.Join(root(), "var/contracts/out")
	for _, p := range []string{bin, filepath.Join(out, "Budget.sol/Budget.json")} {
		if _, err := os.Stat(p); err != nil {
			t.Skip("no local anvil or contract build (var/ is not in git):", p)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skip("no loopback listening here:", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(bin, "--port", strconv.Itoa(port), "--chain-id", signertest.Chain, "--silent")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	w := &world{rpc: eth.NewClient("http://127.0.0.1:" + strconv.Itoa(port))}
	for i := 0; ; i++ {
		if _, err := w.rpc.ChainID(bg); err == nil {
			break
		} else if i == 100 {
			t.Fatal("anvil did not answer")
		}
		time.Sleep(100 * time.Millisecond)
	}
	w.zkb = filepath.Join(t.TempDir(), "zkbudget")
	build := exec.Command("go", "build", "-o", w.zkb, "./cmd/zkbudget")
	build.Dir = filepath.Join(root(), "zk")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build zkbudget: %v %s", err, b)
	}
	verifier := w.deploy(t, filepath.Join(out, "Verifier.sol/Verifier.json"))
	ctor, _ := eth.Encode(verifier, key(t, 1).Addr, key(t, 2).Addr)
	w.contract = w.deploy(t, filepath.Join(out, "Budget.sol/Budget.json"), ctor...)
	h, _ := w.rpc.LatestHeader(bg)
	w.now = h.Timestamp
	return w
}

func (w *world) deploy(t *testing.T, artifact string, ctor ...byte) eth.Address {
	t.Helper()
	raw, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Bytecode struct{ Object string }
	}
	json.Unmarshal(raw, &a)
	code, err := hex.DecodeString(a.Bytecode.Object[2:])
	if err != nil {
		t.Fatal(err)
	}
	k := key(t, 0)
	nonce, _ := w.rpc.PendingNonce(bg, k.Addr)
	h, _ := w.rpc.LatestHeader(bg)
	tx := &eth.Tx{ChainID: big.NewInt(84532), Nonce: nonce, MaxPriorityFeePerGas: big.NewInt(1e9), MaxFeePerGas: new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9)),
		Gas: 6_000_000, Value: new(big.Int), Data: append(code, ctor...)}
	signed, hash, err := tx.Sign(k)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.rpc.SendRaw(bg, signed); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if r, _ := w.rpc.Receipt(bg, hash); r != nil {
			if r.Status != 1 || r.ContractAddress == nil {
				t.Fatalf("deploy %s: %+v", artifact, r)
			}
			return *r.ContractAddress
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("deploy not mined")
	return eth.Address{}
}

func (w *world) relayer(t *testing.T, path string) *Relayer {
	t.Helper()
	cfg := Config{ChainID: 84532, Contract: w.contract, Confirmations: 1, PolicyTTL: 300, Zkbudget: w.zkb, SetupDir: filepath.Join(root(), "zk/setup")}
	r, err := Open(bg, cfg, Keys{Gas: key(t, 3), Issuer: key(t, 1), Policy: key(t, 2)}, w.rpc, path, func() uint64 { return w.now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func (w *world) order(consumer, task string, n int, amount string) (json.RawMessage, *paycontract.PurchaseOrder, string) {
	x := consumer[len(consumer)-1:]
	o := signertest.Order{Consumer: consumer, Agent: "agent-" + x, Wallet: "wallet-" + x, Task: task, From: "0x1111111111111111111111111111111111111111",
		PayTo: signertest.Merchant, Amount: amount, N: n, Now: w.now, Window: 600}
	po := o.PO()
	intent, _ := po.IntentHash()
	return o.Raw(), &po, intent
}

func code(err error) string {
	var r signer.Refusal
	if errors.As(err, &r) {
		return string(r)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// The relayer reserves on a local chain and the Signer's verifier accepts
// exactly that reservation: the same order again is the same transaction;
// another order, another task or too few confirmations is refused; a
// refusal before the transaction writes nothing.
func TestReserveAndVerify(t *testing.T) {
	w := newWorld(t)
	dir := t.TempDir()
	r := w.relayer(t, filepath.Join(dir, "budget.db"))
	if err := r.RegisterTask(bg, "show-a-01", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterTask(bg, "show-a-01", "consumer-b", 1_000_000); code(err) != "TASK_REGISTERED" {
		t.Fatalf("registered for another consumer: %v", err)
	}
	if fi, _ := os.Stat(filepath.Join(dir, "budget.db")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("budget.db %v", fi.Mode())
	}
	raw, po, intent := w.order("consumer-a", "show-a-01", 1, "600000")
	ev, err := r.Reserve(bg, raw)
	if err != nil || ev.IntentHash != intent || ev.Amount != 600000 {
		t.Fatalf("reserve %+v %v", ev, err)
	}
	evRaw, _ := json.Marshal(ev)
	v := &Verifier{RPC: w.rpc, ChainID: 84532, Contract: w.contract, Confirmations: 1}
	if err := v.Verify(bg, intent, po, evRaw); err != nil {
		t.Fatalf("verify: %v", err)
	}
	gas := key(t, 3).Addr
	sent, _ := w.rpc.PendingNonce(bg, gas)
	if again, err := r.Reserve(bg, raw); err != nil || again != ev {
		t.Fatalf("again %+v %v", again, err)
	}
	if n, _ := w.rpc.PendingNonce(bg, gas); n != sent {
		t.Fatal("the same order sent another transaction")
	}

	// The verifier: another intent, task or amount with this evidence; a hash never sent; more confirmations.
	raw2, po2, intent2 := w.order("consumer-a", "show-a-01", 2, "600000")
	_, poB, intentB := w.order("consumer-b", "show-b-01", 3, "600000")
	_, poAmt, intentAmt := w.order("consumer-a", "show-a-01", 1, "600001")
	for name, cs := range map[string]struct {
		intent string
		po     *paycontract.PurchaseOrder
		ev     json.RawMessage
		v      *Verifier
		want   string
	}{
		"another order":            {intent2, po2, evRaw, v, "RESERVATION_MISMATCH"},
		"another task":             {intentB, poB, evRaw, v, "RESERVATION_MISMATCH"},
		"another amount":           {intentAmt, poAmt, evRaw, v, "RESERVATION_MISMATCH"},
		"a hash never sent":        {intent, po, json.RawMessage(`{"tx_hash":"0x` + hex.EncodeToString(make([]byte, 32)) + `"}`), v, "RESERVATION_PENDING"},
		"no evidence":              {intent, po, nil, v, "RESERVATION_MISSING"},
		"too few confirmations":    {intent, po, evRaw, &Verifier{RPC: w.rpc, ChainID: 84532, Contract: w.contract, Confirmations: 1000}, "RESERVATION_UNCONFIRMED"},
		"a lookalike event":        {intent, po, w.lookalike(t, po, intent), v, "RESERVATION_MISMATCH"},
		"another chain's verifier": {intent, po, evRaw, &Verifier{RPC: w.rpc, ChainID: 31337, Contract: w.contract, Confirmations: 1}, "RESERVATION_MISMATCH"},
	} {
		if err := cs.v.Verify(bg, cs.intent, cs.po, cs.ev); code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}

	// Refusals before a transaction: nothing written, nothing sent.
	rawB, _, _ := w.order("consumer-b", "show-a-01", 4, "100000")
	rawOver, _, _ := w.order("consumer-a", "show-a-01", 5, "400001")
	rawNone, _, _ := w.order("consumer-a", "show-a-09", 6, "100000")
	expired := signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "show-a-01", From: "0x1111111111111111111111111111111111111111",
		PayTo: signertest.Merchant, Amount: "100000", N: 10, Now: w.now - 1000, Window: 600}.Raw()
	for name, cs := range map[string]struct {
		raw  json.RawMessage
		want string
	}{
		"B's order on A's task": {rawB, "CONSUMER_MISMATCH"},
		"over the budget left":  {rawOver, "OVER_BUDGET"},
		"an unregistered task":  {rawNone, "TASK_NOT_REGISTERED"},
		"an expired order":      {expired, "ORDER_EXPIRED"},
		"not an order":          {json.RawMessage(`{"version":"x"}`), "ORDER_INVALID"},
	} {
		if _, err := r.Reserve(bg, cs.raw); code(err) != cs.want {
			t.Errorf("%s: %v, want %s", name, err, cs.want)
		}
	}
	var n int
	r.db.QueryRow(`SELECT count(*) FROM reservations`).Scan(&n)
	if got, _ := w.rpc.PendingNonce(bg, gas); n != 1 || got != sent {
		t.Fatalf("refusals wrote %d reservations, nonce %d -> %d", n, sent, got)
	}
	// The rest of the budget is spendable: the task's state followed the chain.
	if _, err := r.Reserve(bg, raw2); code(err) != "OVER_BUDGET" {
		t.Fatalf("600000 of 400000 left: %v", err)
	}
	raw3, po3, intent3 := w.order("consumer-a", "show-a-01", 7, "400000")
	ev3, err := r.Reserve(bg, raw3)
	if err != nil {
		t.Fatal(err)
	}
	ev3Raw, _ := json.Marshal(ev3)
	if err := v.Verify(bg, intent3, po3, ev3Raw); err != nil {
		t.Fatal(err)
	}

	// A transaction that reverted is no reservation.
	reverted := w.revert(t)
	if err := v.Verify(bg, intent3, po3, json.RawMessage(`{"tx_hash":"0x`+hex.EncodeToString(reverted)+`"}`)); code(err) != "RESERVATION_FAILED" {
		t.Fatalf("a reverted transaction: %v", err)
	}

	// A relayer whose state is behind the chain's is stopped by the
	// simulation: nothing written, nothing sent.
	if err := r.RegisterTask(bg, "show-a-02", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	var rOld, cOld string
	r.db.QueryRow(`SELECT r, commitment FROM tasks WHERE task_id = 'show-a-02'`).Scan(&rOld, &cOld)
	raw8, _, _ := w.order("consumer-a", "show-a-02", 8, "100000")
	if _, err := r.Reserve(bg, raw8); err != nil {
		t.Fatal(err)
	}
	r.db.Exec(`UPDATE tasks SET r = ?, commitment = ?, balance = 1000000 WHERE task_id = 'show-a-02'`, rOld, cOld)
	before, _ := w.rpc.PendingNonce(bg, gas)
	raw9, _, _ := w.order("consumer-a", "show-a-02", 9, "100000")
	if _, err := r.Reserve(bg, raw9); code(err) != "RESERVATION_SIMULATION_FAILED" {
		t.Fatalf("a stale state: %v", err)
	}
	r.db.QueryRow(`SELECT count(*) FROM reservations WHERE task_id = 'show-a-02'`).Scan(&n)
	if after, _ := w.rpc.PendingNonce(bg, gas); n != 1 || after != before {
		t.Fatalf("a stale state wrote %d reservations, nonce %d -> %d", n, before, after)
	}
}

// lookalike has another contract emit exactly the Spent the order's
// reservation would (its topics and data; anyone can deploy such a
// contract) and returns that transaction as evidence.
func (w *world) lookalike(t *testing.T, po *paycontract.PurchaseOrder, intent string) json.RawMessage {
	t.Helper()
	// Runtime: copy the calldata to memory, LOG2(memory[64:], word 0, word 1).
	runtime, _ := hex.DecodeString("366000600037602051600051604036036040a200")
	initCode, _ := hex.DecodeString("6014600c60003960146000f3")
	emitter := w.deployCode(t, append(initCode, runtime...))
	ih, _ := IntentBytes(intent)
	amount, _ := po.PaymentBinding.Amount.Units()
	data, _ := eth.Encode(eth.Bytes32(spentTopic), TaskField(84532, w.contract, po.Intent.TaskID), ih, amount, uint64(1), uint64(2))
	hash := w.mine(t, &eth.Tx{To: &emitter, Gas: 100_000, Data: data})
	return json.RawMessage(`{"tx_hash":"0x` + hex.EncodeToString(hash) + `"}`)
}

// deployCode deploys init code from the deployer key.
func (w *world) deployCode(t *testing.T, code []byte) eth.Address {
	t.Helper()
	hash := w.mine(t, &eth.Tx{Gas: 1_000_000, Data: code})
	rc, _ := w.rpc.Receipt(bg, hash)
	if rc.Status != 1 || rc.ContractAddress == nil {
		t.Fatalf("deploy: %+v", rc)
	}
	return *rc.ContractAddress
}

// mine sends tx (its chain, nonce and fees filled in) from the deployer
// key and returns its hash once mined successfully.
func (w *world) mine(t *testing.T, tx *eth.Tx) []byte {
	t.Helper()
	k := key(t, 0)
	tx.ChainID, tx.Value = big.NewInt(84532), new(big.Int)
	tx.Nonce, _ = w.rpc.PendingNonce(bg, k.Addr)
	h, _ := w.rpc.LatestHeader(bg)
	tx.MaxPriorityFeePerGas, tx.MaxFeePerGas = big.NewInt(1e9), new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9))
	signed, hash, _ := tx.Sign(k)
	if _, err := w.rpc.SendRaw(bg, signed); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if rc, _ := w.rpc.Receipt(bg, hash); rc != nil {
			if rc.Status != 1 {
				t.Fatalf("reverted: %+v", rc)
			}
			return hash
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("not mined")
	return nil
}

// revert mines a transaction that reverts (registerTask from a key that is
// not the issuer, its gas fixed so no estimate stops it).
func (w *world) revert(t *testing.T) []byte {
	t.Helper()
	k := key(t, 0)
	data, _ := eth.Call("registerTask(string,uint256)", "show-x-01", uint64(1))
	nonce, _ := w.rpc.PendingNonce(bg, k.Addr)
	h, _ := w.rpc.LatestHeader(bg)
	tx := &eth.Tx{ChainID: big.NewInt(84532), Nonce: nonce, MaxPriorityFeePerGas: big.NewInt(1e9), MaxFeePerGas: new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9)),
		Gas: 200_000, To: &w.contract, Value: new(big.Int), Data: data}
	signed, hash, _ := tx.Sign(k)
	if _, err := w.rpc.SendRaw(bg, signed); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if rc, _ := w.rpc.Receipt(bg, hash); rc != nil {
			if rc.Status != 0 {
				t.Fatal("it did not revert")
			}
			return hash
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("not mined")
	return nil
}

// A crash after the transaction is written and before it is sent: the
// task is busy for every other order; the same order, after a restart,
// sends that very transaction, and the chain has one spend.
func TestReserveResumesAfterACrash(t *testing.T) {
	w := newWorld(t)
	// Bounded: a transaction that is never sent fails the test, not hangs it.
	ctx, cancel := context.WithTimeout(bg, 60*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "budget.db")
	r := w.relayer(t, path)
	if err := r.RegisterTask(ctx, "show-a-01", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	raw, po, intent := w.order("consumer-a", "show-a-01", 1, "500000")
	hook = func(stage string) error {
		if stage == "after-write" {
			return errors.New("crash")
		}
		return nil
	}
	_, err := r.Reserve(ctx, raw)
	hook = nil
	if err == nil {
		t.Fatal("no crash")
	}
	gas := key(t, 3).Addr
	before, _ := w.rpc.PendingNonce(bg, gas)
	other, _, _ := w.order("consumer-a", "show-a-01", 2, "100000")
	if _, err := r.Reserve(ctx, other); code(err) != "TASK_BUSY" {
		t.Fatalf("another order while one is open: %v", err)
	}
	r.Close()
	r = w.relayer(t, path) // a restart
	var txHash string
	r.db.QueryRow(`SELECT tx_hash FROM reservations WHERE intent_hash = ?`, intent).Scan(&txHash)
	ev, err := r.Reserve(ctx, raw)
	if err != nil || ev.TxHash != "0x"+txHash {
		t.Fatalf("resume %+v %v (written %s)", ev, err, txHash)
	}
	if after, _ := w.rpc.PendingNonce(bg, gas); after != before+1 {
		t.Fatalf("gas nonce %d -> %d", before, after)
	}
	logs, err := w.rpc.Logs(bg, w.contract, spentTopic, 0, ev.BlockNumber)
	if err != nil || len(logs) != 1 {
		t.Fatalf("spends on chain: %d %v", len(logs), err)
	}
	evRaw, _ := json.Marshal(ev)
	if err := (&Verifier{RPC: w.rpc, ChainID: 84532, Contract: w.contract, Confirmations: 1}).Verify(bg, intent, po, evRaw); err != nil {
		t.Fatal(err)
	}
}

// The policy digest computed here is the contract's.
func TestPolicyDigestIsTheContracts(t *testing.T) {
	w := newWorld(t)
	var ih eth.Bytes32
	ih[0] = 0xab
	task := TaskField(84532, w.contract, "show-a-01")
	n, c := big.NewInt(7), big.NewInt(9)
	data, _ := eth.Call("policyDigest(uint256,bytes32,uint256,uint256,uint256,uint256)", task, ih, uint64(5), n, c, uint64(4102444800))
	got, err := w.rpc.Call(bg, eth.Msg{From: key(t, 0).Addr, To: w.contract, Data: data})
	want, _ := PolicyDigest(84532, w.contract, task, ih, 5, n, c, 4102444800, new(big.Int))
	if err != nil || hex.EncodeToString(got) != hex.EncodeToString(want) {
		t.Fatalf("digest %x, contract %x %v", want, got, err)
	}
	tf, _ := eth.Call("taskField(string)", "show-a-01")
	got, _ = w.rpc.Call(bg, eth.Msg{From: key(t, 0).Addr, To: w.contract, Data: tf})
	if new(big.Int).SetBytes(got).Cmp(task) != 0 {
		t.Fatalf("task field %x, contract %x", task, got)
	}
}

// The payment Signer with this verifier: it signs an order only once the
// relayer's reservation of that very order is on chain.
func TestSignerSignsOnlyAReservedOrder(t *testing.T) {
	w := newWorld(t)
	r := w.relayer(t, filepath.Join(t.TempDir(), "budget.db"))
	if err := r.RegisterTask(bg, "show-a-01", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	wallet := secp256k1.PrivKeyFromBytes(eth.Keccak256([]byte("budget test wallet a")))
	walletB := secp256k1.PrivKeyFromBytes(eth.Keccak256([]byte("budget test wallet b")))
	a, b := signer.Address(wallet.PubKey()), signer.Address(walletB.PubKey())
	cfg, err := signer.ParseConfig(signertest.Config("/unused", 0, w.now, a, b))
	if err != nil {
		t.Fatal(err)
	}
	v := &Verifier{RPC: w.rpc, ChainID: 84532, Contract: w.contract, Confirmations: 1}
	core, err := signer.NewCore(bg, cfg, map[string]*secp256k1.PrivateKey{a: wallet, b: walletB}, filepath.Join(t.TempDir(), "signer.db"),
		func() uint64 { return w.now }, v)
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	order := func(n int) json.RawMessage {
		return signertest.Order{Consumer: "consumer-a", Agent: "agent-a", Wallet: "wallet-a", Task: "show-a-01", From: a, PayTo: signertest.Merchant,
			Amount: "500000", N: n, Now: w.now, Window: 60}.Raw()
	}
	auth := signer.Authorization{Method: "delegation", DelegationID: "dlg-a", DelegationVersion: 1}
	o1, o2 := order(1), order(2)
	if _, err := core.Sign(bg, signer.SignRequest{RequestID: "request-0000000801", Authorization: auth, Order: o1}); code(err) != "RESERVATION_MISSING" {
		t.Fatalf("no reservation: %v", err)
	}
	ev, err := r.Reserve(bg, o1)
	if err != nil {
		t.Fatal(err)
	}
	evRaw, _ := json.Marshal(ev)
	if _, err := core.Sign(bg, signer.SignRequest{RequestID: "request-0000000802", Authorization: auth, Order: o2, Reservation: evRaw}); code(err) != "RESERVATION_MISMATCH" {
		t.Fatalf("another order's reservation: %v", err)
	}
	rep, err := core.Sign(bg, signer.SignRequest{RequestID: "request-0000000803", Authorization: auth, Order: o1, Reservation: evRaw})
	if err != nil || rep.State != "signed" {
		t.Fatalf("sign %+v %v", rep, err)
	}
}

// call calls a node method the client has no wrapper for (anvil's own).
func (w *world) call(t *testing.T, method string, params ...any) {
	t.Helper()
	if params == nil {
		params = []any{}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	res, err := http.Post(w.rpc.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

// crashAfterWrite reserves raw and stops after its transaction is written,
// before it is sent; it returns the written transaction's nonce.
func crashAfterWrite(t *testing.T, ctx context.Context, r *Relayer, raw json.RawMessage, intent string) uint64 {
	t.Helper()
	hook = func(stage string) error {
		if stage == "after-write" {
			return errors.New("crash")
		}
		return nil
	}
	_, err := r.Reserve(ctx, raw)
	hook = nil
	if err == nil {
		t.Fatal("no crash")
	}
	var n uint64
	if err := r.db.QueryRow(`SELECT nonce FROM reservations WHERE intent_hash = ? AND state = 'sent'`, intent).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A reservation that reverts (written, sent only after its policy deadline)
// leaves the task's state as it was, and the next order on the task, which
// proves the same nullifier, is reserved.
func TestReserveAfterARevert(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(bg, 60*time.Second)
	defer cancel()
	r := w.relayer(t, filepath.Join(t.TempDir(), "budget.db"))
	if err := r.RegisterTask(ctx, "show-a-01", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	raw, _, intent := w.order("consumer-a", "show-a-01", 1, "500000")
	crashAfterWrite(t, ctx, r, raw, intent)
	w.call(t, "evm_increaseTime", 400) // past the 300 s policy deadline
	w.call(t, "evm_mine")
	h, _ := w.rpc.LatestHeader(bg)
	w.now = h.Timestamp
	if _, err := r.Reserve(ctx, raw); code(err) != "RESERVATION_FAILED" {
		t.Fatalf("sent after its deadline: %v", err)
	}
	raw2, po2, intent2 := w.order("consumer-a", "show-a-01", 2, "500000")
	ev, err := r.Reserve(ctx, raw2)
	if err != nil {
		t.Fatalf("the next order on the task: %v", err)
	}
	evRaw, _ := json.Marshal(ev)
	if err := (&Verifier{RPC: w.rpc, ChainID: 84532, Contract: w.contract, Confirmations: 1}).Verify(bg, intent2, po2, evRaw); err != nil {
		t.Fatal(err)
	}
}

// After a restart, a new reservation never takes the nonce of one written
// before the crash: that one is sent again first, and both are reserved.
func TestResendBeforeANewNonce(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(bg, 60*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "budget.db")
	r := w.relayer(t, path)
	r.wait = 10 * time.Second
	for _, task := range []string{"show-a-01", "show-a-02"} {
		if err := r.RegisterTask(ctx, task, "consumer-a", 1_000_000); err != nil {
			t.Fatal(err)
		}
	}
	gas := key(t, 3).Addr
	before, _ := w.rpc.PendingNonce(bg, gas)
	rawA, _, intentA := w.order("consumer-a", "show-a-01", 1, "500000")
	crashAfterWrite(t, ctx, r, rawA, intentA)
	r.Close()
	r = w.relayer(t, path)
	r.wait = 10 * time.Second
	rawB, _, _ := w.order("consumer-a", "show-a-02", 2, "500000")
	if _, err := r.Reserve(ctx, rawB); err != nil {
		t.Fatalf("B: %v", err)
	}
	if _, err := r.Reserve(ctx, rawA); err != nil {
		t.Fatalf("A, written before the crash: %v", err)
	}
	if after, _ := w.rpc.PendingNonce(bg, gas); after != before+2 {
		t.Fatalf("gas nonce %d -> %d", before, after)
	}
}

// A written reservation whose nonce another transaction used can never be
// mined: it is failed, not pending forever, and the task takes the next.
func TestDroppedReservationFails(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(bg, 60*time.Second)
	defer cancel()
	r := w.relayer(t, filepath.Join(t.TempDir(), "budget.db"))
	r.wait = 10 * time.Second
	if err := r.RegisterTask(ctx, "show-a-01", "consumer-a", 1_000_000); err != nil {
		t.Fatal(err)
	}
	raw, _, intent := w.order("consumer-a", "show-a-01", 1, "500000")
	n := crashAfterWrite(t, ctx, r, raw, intent)
	// Another transaction of the gas key at that nonce (an operator's, say).
	h, _ := w.rpc.LatestHeader(bg)
	to := eth.Address{0xde, 0xad}
	tx := &eth.Tx{ChainID: big.NewInt(84532), Nonce: n, MaxPriorityFeePerGas: big.NewInt(1e9), MaxFeePerGas: new(big.Int).Add(new(big.Int).Mul(h.BaseFee, big.NewInt(2)), big.NewInt(1e9)),
		Gas: 21000, To: &to, Value: big.NewInt(1)}
	signed, _, _ := tx.Sign(key(t, 3))
	if _, err := w.rpc.SendRaw(bg, signed); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Reserve(ctx, raw); code(err) != "RESERVATION_FAILED" {
		t.Fatalf("its nonce used by another: %v", err)
	}
	raw2, _, _ := w.order("consumer-a", "show-a-01", 2, "500000")
	if _, err := r.Reserve(ctx, raw2); err != nil {
		t.Fatalf("the next order on the task: %v", err)
	}
}

// A written transaction whose resend every pool refuses (its fee cap is
// now below the base fee): a new transaction never takes a nonce behind
// that gap (RELAYER_NONCE_BLOCKED, nothing written); once the chain's time
// passes its deadline it is decided failed before the next one is
// prepared, so neither its task nor the nonce stays blocked.
func TestBlockedNonceClears(t *testing.T) {
	w := newWorld(t)
	ctx, cancel := context.WithTimeout(bg, 90*time.Second)
	defer cancel()
	r := w.relayer(t, filepath.Join(t.TempDir(), "budget.db"))
	r.wait = 10 * time.Second
	for _, task := range []string{"show-a-01", "show-a-02"} {
		if err := r.RegisterTask(ctx, task, "consumer-a", 1_000_000); err != nil {
			t.Fatal(err)
		}
	}
	rawA, _, intentA := w.order("consumer-a", "show-a-01", 1, "500000")
	crashAfterWrite(t, ctx, r, rawA, intentA)
	w.call(t, "anvil_setNextBlockBaseFeePerGas", "0x"+strconv.FormatInt(1000e9, 16))
	w.call(t, "evm_mine")
	rawB, _, _ := w.order("consumer-a", "show-a-02", 2, "500000")
	if _, err := r.Reserve(ctx, rawB); code(err) != "RELAYER_NONCE_BLOCKED" {
		t.Fatalf("behind a gap: %v", err)
	}
	var n int
	r.db.QueryRow(`SELECT count(*) FROM reservations`).Scan(&n)
	if n != 1 {
		t.Fatalf("%d reservations written", n)
	}
	w.call(t, "evm_increaseTime", 400) // past A's policy deadline
	w.call(t, "evm_mine")
	h, _ := w.rpc.LatestHeader(bg)
	w.now = h.Timestamp
	rawA2, _, _ := w.order("consumer-a", "show-a-01", 3, "500000")
	if _, err := r.Reserve(ctx, rawA2); err != nil {
		t.Fatalf("A's task after A's deadline: %v", err)
	}
	rawB2, _, _ := w.order("consumer-a", "show-a-02", 4, "500000")
	if _, err := r.Reserve(ctx, rawB2); err != nil {
		t.Fatalf("B's task: %v", err)
	}
	if _, err := r.Reserve(ctx, rawA); code(err) != "RESERVATION_FAILED" {
		t.Fatalf("A: %v", err)
	}
}
