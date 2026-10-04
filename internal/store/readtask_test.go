package store

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

// ReadTask: the binding stored with the task, its wallet's evidence mode
// from the task's own registry snapshot; the caller's grant is asked with
// the stored consumer only; unknown, not started and not visible are the
// same refusal; a read writes nothing; a stored binding the snapshot does
// not hold is corrupt, never filled in from anywhere else.
func TestReadTask(t *testing.T) {
	e := newEnv(t)
	s := e.open()
	e.start(s, "task-a")
	submit(t, s, sessA, e.base)
	all := func(string) bool { return true }

	rec, err := s.ReadTask("task-a", all)
	want := TaskBinding{TaskID: "task-a", ConsumerID: "consumer-a", AgentID: "agent-a", WalletID: "wallet-a",
		Payer: "0x1111111111111111111111111111111111111111", WalletEvidenceMode: "fixture"}
	if err != nil || rec.Binding != want || len(rec.Events) == 0 || rec.Task.AsOfSequence != rec.Events[len(rec.Events)-1].Sequence {
		t.Fatalf("%v %+v", err, rec)
	}
	if v := task(t, s, sessA); !reflect.DeepEqual(v, rec.Task) {
		t.Errorf("projection differs from Task: %+v vs %+v", rec.Task, v)
	}

	var asked []string
	if _, err := s.ReadTask("task-a", func(c string) bool { asked = append(asked, c); return c == "consumer-b" }); Code(err) != "OWNERSHIP_MISMATCH" {
		t.Errorf("not visible: %v", err)
	}
	if !slices.Equal(asked, []string{"consumer-a"}) {
		t.Errorf("the grant was asked with %v, not the stored consumer", asked)
	}
	for _, id := range []string{"task-z", "task-a-next"} {
		if _, err := s.ReadTask(id, all); Code(err) != "OWNERSHIP_MISMATCH" {
			t.Errorf("%s: %v", id, err)
		}
	}

	before := fingerprint(t, e.path)
	e.clock.Add(3600) // past the Ask's deadline: a read must not expire it
	if rec, err := s.ReadTask("task-a", all); err != nil || rec.Task.Purchases[0].ApprovalClosed != "" {
		t.Fatalf("%v %+v", err, rec.Task)
	}
	if fingerprint(t, e.path) != before {
		t.Error("ReadTask wrote to the database")
	}

	if _, err := rawDB(t, e.path).Exec(`UPDATE tasks SET wallet_id = 'wallet-b' WHERE task_id = 'task-a'`); err != nil {
		t.Fatal(err)
	}
	var ce *CorruptError
	if _, err := s.ReadTask("task-a", all); !errors.As(err, &ce) || ce.Check != "binding" {
		t.Errorf("a stored binding its snapshot does not hold: %v", err)
	}
}
