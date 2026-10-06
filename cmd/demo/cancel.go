package main

// POST /api/host/tasks/{task_id}/cancel (docs/demo-operation-design.md
// section 6.1, slice 3.4): the operator ends a stored task whose run is no
// longer running, through the Store's own Cancel; no process is involved.
// A running run is stopped with its stop, never here. The answer has fixed
// fields and codes only: the operator reads no task content.

import (
	"net/http"
	"regexp"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// cancelGoneCode is the 503 code when a task seen a moment ago is not there
// on the re-read (tasks are never deleted: the Store is not what it was).
// cancelOddCode: the Store refused as already ended, yet the re-read shows
// no end. Both are this file's only codes beyond the Store's own.
const (
	cancelGoneCode = "TASK_NOT_FOUND"
	cancelOddCode  = "TASK_ALREADY_ENDED"
)

var taskIDFormat = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// cancelFault is a test double's hook before each Store step ("read",
// "write", "reread"); nil in production.
var cancelFault func(stage string) error

func (c *control) readForCancel(id, stage string) (store.TaskRecord, error) {
	if cancelFault != nil {
		if err := cancelFault(stage); err != nil {
			return store.TaskRecord{}, err
		}
	}
	return c.st.ReadTask(id, func(string) bool { return true })
}

func storeDown(w http.ResponseWriter, code string) {
	reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": code})
}

// answerEnded answers a task that needs no cancel, in section 6.1's order: the
// first ending event decides (only one can exist), then a Deny. ok is
// false when the task can still be cancelled.
func answerEnded(w http.ResponseWriter, rec store.TaskRecord) bool {
	for _, e := range rec.Events {
		switch e.EventType {
		case "TASK_CANCELLED":
			reply(w, http.StatusOK, map[string]any{"state": "cancelled", "already": true})
			return true
		case "TASK_TIMED_OUT":
			reply(w, http.StatusConflict, map[string]string{"error": "already_ended", "end": "TASK_TIMED_OUT"})
			return true
		}
	}
	if rec.Task.Status == "denied" { // a Deny is final; its reason stays
		reply(w, http.StatusConflict, map[string]string{"error": "not_eligible", "status": "denied"})
		return true
	}
	return false
}

// cancelTask holds c.mu from the first read to the answer, as start does:
// no run of this task can start meanwhile (a start only takes a new task),
// and the active run cannot end unseen. Store calls need no c.mu, so
// nothing waits on it while it is held.
func (c *control) cancelTask(w http.ResponseWriter, r *http.Request, a *api) {
	var empty struct{}
	if !postGuard(w, r, a, &empty) {
		return
	}
	id := r.PathValue("task_id")
	if !taskIDFormat.MatchString(id) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	rec, err := c.readForCancel(id, "read")
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH": // every task is visible here: it is not stored
		fail(w, http.StatusNotFound, "not_found")
		return
	case err != nil:
		storeDown(w, store.CodeOf(err))
		return
	}
	if c.active != nil && c.active.run.TaskID == id { // running or stopping: its stop ends it
		fail(w, http.StatusConflict, "run_active")
		return
	}
	if answerEnded(w, rec) {
		return
	}
	if cancelFault != nil {
		if err := cancelFault("write"); err != nil {
			storeDown(w, store.CodeOf(err))
			return
		}
	}
	err = c.st.Cancel(id)
	switch store.Code(err) {
	case "":
		if err != nil {
			storeDown(w, store.CodeOf(err))
			return
		}
		reply(w, http.StatusOK, map[string]any{"state": "cancelled"})
		return
	case "TASK_ALREADY_ENDED": // ended by another writer meanwhile: answer what was committed
	case "UNKNOWN_TASK":
		storeDown(w, cancelGoneCode)
		return
	default:
		storeDown(w, store.CodeOf(err))
		return
	}
	rec, err = c.readForCancel(id, "reread")
	switch {
	case store.Code(err) == "OWNERSHIP_MISMATCH":
		storeDown(w, cancelGoneCode)
	case err != nil:
		storeDown(w, store.CodeOf(err))
	case !answerEnded(w, rec):
		storeDown(w, cancelOddCode)
	}
}
