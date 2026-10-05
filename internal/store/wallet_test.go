package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

const otherAddress = "0x7777777777777777777777777777777777777777"

// handover is a later registry, valid on its own, in which consumer-b's
// wallet takes over something consumer-a has already used: its payer
// ("payer") or its wallet_id ("wallet_id"), never both, so each case can only
// trip one rule.
func handover(t *testing.T, base contracts.RegistryConfig, what string) contracts.RegistryConfig {
	t.Helper()
	var r contracts.RegistryConfig
	raw, _ := json.Marshal(base)
	if err := json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	a, b := &r.Wallets[0], &r.Wallets[1] // wallet-a 0x1111..., wallet-b 0x2222...
	switch what {
	case "payer":
		a.From, b.From = "0x9999999999999999999999999999999999999999", a.From
	case "wallet_id":
		a.WalletID, b.WalletID, b.From = "wallet-z", "wallet-a", otherAddress
		for i := range r.Consumers {
			r.Consumers[i].WalletID = map[string]string{"consumer-a": "wallet-z", "consumer-b": "wallet-a"}[r.Consumers[i].ConsumerID]
		}
		for i := range r.Tasks {
			r.Tasks[i].WalletID = map[string]string{"consumer-a": "wallet-z", "consumer-b": "wallet-a"}[r.Tasks[i].ConsumerID]
		}
	default:
		t.Fatalf("handover %q", what)
	}
	if _, err := contracts.NewRegistry(r); err != nil {
		t.Fatalf("the %s handover registry must be valid on its own: %v", what, err)
	}
	return r
}

var walletCodes = map[string]string{"payer": "PAYER_IN_USE", "wallet_id": "WALLET_ID_IN_USE"}

// REV2 at StartTask: consumer-a used X under the first registry; a later
// registry gives X to consumer-b. Starting B's task is refused and writes
// nothing.
func TestWalletOwnershipAtStart(t *testing.T) {
	for what, code := range walletCodes {
		t.Run(what, func(t *testing.T) {
			e := newEnv(t)
			s := e.open()
			e.start(s, "task-a")
			s.Close()
			e.reg = handover(t, e.reg, what)
			s = e.open() // task-a still verifies against its own snapshot
			before := fingerprint(t, e.path)
			err := s.StartTask(e.setup("task-b"))
			wantCode(t, err, code)
			if !strings.Contains(err.Error(), what) || !strings.Contains(err.Error(), "consumer-a") {
				t.Fatalf("refusal does not name the %s and its owner: %v", what, err)
			}
			if fingerprint(t, e.path) != before {
				t.Fatal("a refused StartTask wrote")
			}
		})
	}
	// The legitimate cases stay open: one consumer reuses its wallet across
	// tasks, and consumers with their own wallets coexist.
	e := newEnv(t)
	s := e.open()
	for _, id := range []string{"task-a", "task-a-next", "task-b"} {
		if err := s.StartTask(e.setup(id)); err != nil {
			t.Fatalf("StartTask %s: %v", id, err)
		}
	}
	consistent(t, s, sessB)
}

// conflictHistory produces a database in which every row was written by the
// real StartTask and is valid on its own, yet two consumers own the same
// wallet: task-a under the first registry here, task-b under the handover
// registry in a separate database, merged in afterwards.
func conflictHistory(t *testing.T, what string) *env {
	t.Helper()
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	s.Close()

	other := newEnv(t)
	other.reg = handover(t, e.reg, what)
	other.clock = e.clock
	o := other.open()
	other.start(o, "task-b")
	o.Close()
	// Before the merge each database passes recovery on its own.
	e.open().Close()
	other.open().Close()

	db := rawDB(t, e.path)
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		fmt.Sprintf(`ATTACH DATABASE '%s' AS b`, filepath.ToSlash(other.path)),
		`INSERT OR IGNORE INTO config_snapshots SELECT * FROM b.config_snapshots`,
		`INSERT INTO tasks SELECT * FROM b.tasks`,
		`INSERT INTO events SELECT * FROM b.events`,
		`DETACH DATABASE b`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	return e
}

// REV2 at recovery: the merged history has a valid snapshot, task and event
// chain for each task; only the cross-consumer ownership is wrong, and only
// the new guard may refuse it.
func TestWalletOwnershipAtRecovery(t *testing.T) {
	for what := range walletCodes {
		t.Run(what, func(t *testing.T) {
			e := conflictHistory(t, what)
			var ce *CorruptError
			err := openErr(e)
			if !errors.As(err, &ce) || ce.Check != "wallet" || ce.TaskID != "task-b" || !strings.Contains(ce.Detail, what) {
				t.Fatalf("Open: %v; want the wallet check to refuse task-b's %s", err, what)
			}
		})
	}
}

// Two hosts with conflicting but individually valid registries race to give
// the same payer to two consumers: the check runs under the write lock, so
// at most one wins.
func TestWalletOwnershipRace(t *testing.T) {
	for round := range 10 {
		e := newEnv(t)
		s1 := e.open()
		second := *e
		second.reg = handover(t, e.reg, "payer")
		s2 := second.open()
		var wg sync.WaitGroup
		var errA, errB error
		wg.Go(func() { errA = s1.StartTask(e.setup("task-a")) })
		wg.Go(func() { errB = s2.StartTask(second.setup("task-b")) })
		wg.Wait()
		switch {
		case errA == nil && errB == nil:
			t.Fatalf("round %d: both consumers took the same payer", round)
		case errA != nil && errB != nil:
			t.Fatalf("round %d: both refused: %v / %v", round, errA, errB)
		case errA != nil && Code(errA) != "PAYER_IN_USE", errB != nil && Code(errB) != "PAYER_IN_USE":
			t.Fatalf("round %d: unexpected refusal %v / %v", round, errA, errB)
		}
		if err := s1.verify(schemaVersion); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
	}
}
