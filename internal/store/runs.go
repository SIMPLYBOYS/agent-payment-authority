package store

// Run coordination of the demo host (docs/demo-operation-design.md sections
// 13.2-13.4, 13.8-13.10): one host_runs row per accepted run, in the same
// file and WAL as the tasks. These functions touch host_runs only; they read
// tasks to allocate, and never write an event or change a task.

import (
	"cmp"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// RunOutcomes are the outcome codes a run may end with: the buyer's fixed
// Outcome values (internal/buyer/runner.go, checked by its sync test) and
// "skipped" for a run whose process never started.
var RunOutcomes = []string{
	"ok", "not_installed", "start_failed", "not_logged_in", "rate_limited", "timeout", "cancelled",
	"output_overflow", "parse_error", "nonzero_exit", "missing_result", "cli_error", "orphaned_output",
	"input_error", "config_mismatch", "turn_limit", "store_unavailable", "skipped",
}

// RunDetails are the fixed detail codes besides prepare_failed:<code>
// (section 13.11.1): the host's, the buyer's (runner stops, Watch, Gate,
// init_missing; checked by sync tests) and detail_unlisted.
var RunDetails = []string{
	"", "host_error", "scratch_home_failed", "task_reserved_by_run",
	"ambiguous_json", "assistant_messages", "input_failed", "input_partial",
	"hook_message", "init_duplicate", "prepare_failed",
	"init_malformed", "tools", "mcp_servers", "permission_mode", "cli_version", "plugins", "skills", "agents",
	"auth_missing", "auth_unconfirmed", "init_missing",
	"detail_unlisted",
}

// StoreCodes are this package's refusal codes, and STORE_ERROR, the fixed
// stand-in for an error without one. With contracts.ReasonCodes they are
// the codes prepare_failed:<code> may carry. A sync test keeps the list.
var StoreCodes = []string{
	"CLOCK_INVALID", "CLOCK_REGRESSION", "IDEMPOTENCY_CONFLICT", "INVALID_PURCHASE", "INVALID_TASK_SETUP",
	"OWNERSHIP_MISMATCH", "QUOTE_NOT_OFFERED", "TASK_ALREADY_ENDED", "TASK_ALREADY_STARTED", "UNKNOWN_TASK",
	"WALLET_BINDING_CHANGED", "WALLET_ID_IN_USE", "PAYER_IN_USE",
	"SCHEMA_UPGRADE_REQUIRED", "NO_FREE_TASK", "RUN_NOT_OPEN", "INVALID_RUN_RECORD",
	"CONTRACT_VERSION_MISMATCH", "DELEGATION_UNAVAILABLE", "INVALID_DELEGATION", "INVALID_APPROVAL", "INTENT_MISMATCH",
	"HUMAN_APPROVAL_UNAVAILABLE", "GRANT_METHOD_NOT_ACCEPTED", "DELEGATION_REF_REQUIRED", "UNKNOWN_PURCHASE", "PAYMENT_NOT_ALLOWED",
	"UNKNOWN_PAYMENT", "PAYMENT_FINAL", "INVALID_PAYMENT_UPDATE",
	"ATTEMPT_STATE", "DECISION_CONFLICT", "SESSION_CLOSED", // A2's approval attempts
	"STORE_ERROR",
}

// CodeOf is Code(err), or STORE_ERROR for an error without a code: a fixed
// value, never the error's text, in every output (section 13.11.1).
func CodeOf(err error) string {
	if c := Code(err); c != "" {
		return c
	}
	return "STORE_ERROR"
}

// ValidDetail says whether d is a listed detail code or prepare_failed:<a
// listed Store or contract code>, at most 64 bytes.
func ValidDetail(d string) bool {
	if len(d) > 64 {
		return false
	}
	if c, ok := strings.CutPrefix(d, "prepare_failed:"); ok {
		return slices.Contains(StoreCodes, c) || slices.Contains(contracts.ReasonCodes, c)
	}
	return slices.Contains(RunDetails, d)
}

// Run is one host_runs row. The binding and scenario are what the host
// trusted when it accepted the run.
type Run struct {
	RunID, RequestID, Scenario, ScenarioDigest, BudgetInitial string
	TaskID, ConsumerID, AgentID, WalletID, Payer              string
	State                                                     string // accepted | interrupted | ended
	AcceptedAt, InterruptedAt, EndedAt                        uint64
	Outcome, Detail                                           string
	TaskStarted                                               bool
	StoreEnd, StoreEndError                                   string
	ContractVersion                                           string // the trusted catalog's: apa.m1.v2 | apa.pay.v1
	DelegationID                                              string // apa.pay.v1: the confirmed delegation the run starts with
}

// RunEnd is what a run reports when its process has ended (T4).
type RunEnd struct {
	Outcome, Detail         string
	TaskStarted             bool
	StoreEnd, StoreEndError string
}

// RunRequest is a new run to accept (T1). Candidates are the trusted task
// IDs this scenario's consumer may get, in allocation order.
type RunRequest struct {
	RequestID, Scenario, ScenarioDigest, BudgetInitial, ConsumerID string
	Candidates                                                     []string
	ContractVersion, DelegationID                                  string // "" = apa.m1.v2, no delegation
}

var (
	requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{16,64}$`)
	storeCodePattern = regexp.MustCompile(`^[A-Z_]{1,64}$`)
)

// runRow is a scanned row, NULLs kept, for validRun.
type runRow struct {
	r                                        Run
	interruptedAt, endedAt                   sql.NullInt64
	outcome, detail, storeEnd, storeEndError sql.NullString
	taskStarted                              sql.NullInt64
}

const runCols = runColsBase + `, contract_version, delegation_id`

const runColsBase = `run_id, request_id, scenario, scenario_digest, budget_initial, task_id, consumer_id, agent_id, wallet_id, payer,
  state, accepted_at, interrupted_at, ended_at, outcome, detail, task_started, store_end, store_end_error`

func scanRun(sc interface{ Scan(...any) error }) (runRow, error) {
	var w runRow
	var dlg sql.NullString
	err := sc.Scan(&w.r.RunID, &w.r.RequestID, &w.r.Scenario, &w.r.ScenarioDigest, &w.r.BudgetInitial, &w.r.TaskID,
		&w.r.ConsumerID, &w.r.AgentID, &w.r.WalletID, &w.r.Payer, &w.r.State, &w.r.AcceptedAt,
		&w.interruptedAt, &w.endedAt, &w.outcome, &w.detail, &w.taskStarted, &w.storeEnd, &w.storeEndError, &w.r.ContractVersion, &dlg)
	w.r.DelegationID = dlg.String
	w.r.InterruptedAt, w.r.EndedAt = uint64(w.interruptedAt.Int64), uint64(w.endedAt.Int64)
	w.r.Outcome, w.r.Detail, w.r.StoreEnd, w.r.StoreEndError = w.outcome.String, w.detail.String, w.storeEnd.String, w.storeEndError.String
	w.r.TaskStarted = w.taskStarted.Int64 == 1
	return w, err
}

// validRun is the one rule set for a row (section 13.9.1, 13.10): each
// state's required and empty fields, the code lists and the combinations.
// It returns "" or what is wrong; the same function guards EndRun and the
// recovery check, which also catches rows that bypassed the CHECKs.
func validRun(w runRow) string {
	r := w.r
	if r.RunID == "" || !requestIDPattern.MatchString(r.RequestID) || r.Scenario == "" || r.ScenarioDigest == "" ||
		r.TaskID == "" || r.ConsumerID == "" || r.AgentID == "" || r.WalletID == "" || r.Payer == "" || r.AcceptedAt == 0 {
		return "a binding or acceptance field is missing"
	}
	switch r.ContractVersion {
	case contracts.Version:
		if r.DelegationID != "" {
			return "an apa.m1.v2 run has a delegation"
		}
	case paycontract.Version, paycontract.V2:
		if r.DelegationID == "" {
			return "a pay-family run has no delegation"
		}
	default:
		return "unknown contract version"
	}
	result := w.outcome.Valid || w.detail.Valid || w.taskStarted.Valid || w.storeEnd.Valid || w.storeEndError.Valid
	switch r.State {
	case "accepted", "interrupted":
		if w.endedAt.Valid || result {
			return r.State + " has end fields"
		}
		if (r.State == "interrupted") != w.interruptedAt.Valid {
			return "interrupted_at does not match the state"
		}
		return ""
	case "ended":
	default:
		return "unknown state"
	}
	switch {
	case w.interruptedAt.Valid:
		return "ended has interrupted_at"
	case !w.endedAt.Valid || r.EndedAt < r.AcceptedAt:
		return "ended_at missing or before accepted_at"
	case !w.taskStarted.Valid || (w.taskStarted.Int64 != 0 && w.taskStarted.Int64 != 1):
		return "task_started must be 0 or 1"
	case !w.outcome.Valid || !slices.Contains(RunOutcomes, r.Outcome):
		return "outcome is not a listed code"
	case !w.detail.Valid || !ValidDetail(r.Detail):
		return "detail is not a listed code"
	case !w.storeEnd.Valid || !slices.Contains([]string{"", "TASK_CANCELLED", "TASK_TIMED_OUT"}, r.StoreEnd):
		return "store_end is not '', TASK_CANCELLED or TASK_TIMED_OUT"
	case w.storeEndError.Valid && (!storeCodePattern.MatchString(r.StoreEndError) || !slices.Contains(StoreCodes, r.StoreEndError) && !slices.Contains(contracts.ReasonCodes, r.StoreEndError)):
		return "store_end_error is not a listed code"
	case r.StoreEndError != "" && r.StoreEnd != "":
		return "store_end and store_end_error are both set"
	case r.Outcome == "skipped" && (r.TaskStarted || r.StoreEnd != ""):
		return "skipped with a task or an end"
	case r.StoreEnd == "TASK_CANCELLED" && r.Outcome != "cancelled", r.StoreEnd == "TASK_TIMED_OUT" && r.Outcome != "timeout":
		return "store_end does not match the outcome"
	case r.StoreEnd != "" && !r.TaskStarted:
		return "store_end without a task"
	case (r.Outcome == "cancelled" || r.Outcome == "timeout") && r.TaskStarted && (r.StoreEnd == "") == (r.StoreEndError == ""):
		return "a cancelled or timed-out task needs exactly one of store_end and store_end_error"
	}
	return ""
}

// verifyRuns is the recovery check of host_runs (section 13.8 R1, 13.9.1),
// inside verify's read transaction: every row valid, its binding equal to
// the stored task's, task_started and store_end matching tasks and events.
// It reads no current setting.
func verifyRuns(tx *sql.Tx, version int) error {
	cols := runCols
	if version < 3 { // before schema 3 every run was apa.m1.v2, without a delegation
		cols = runColsBase + `, 'apa.m1.v2', NULL`
	}
	rows, err := tx.Query(`SELECT ` + cols + ` FROM host_runs ORDER BY run_id`)
	if err != nil {
		return readErr("host_runs", "", err)
	}
	var all []runRow
	for rows.Next() {
		w, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return readErr("host_runs", "", err)
		}
		all = append(all, w)
	}
	if err := drained(rows); err != nil {
		return readErr("host_runs", "", err)
	}
	for _, w := range all {
		r := w.r
		bad := func(format string, a ...any) error {
			return &CorruptError{Check: "host_runs", TaskID: r.TaskID, Detail: "run " + r.RunID + ": " + fmt.Sprintf(format, a...)}
		}
		if why := validRun(w); why != "" {
			return bad("%s", why)
		}
		why, err := runVsStore(tx, r, version)
		if err != nil {
			return readErr("host_runs", r.TaskID, err)
		}
		if why != "" {
			return bad("%s", why)
		}
	}
	return nil
}

// runVsStore is the one rule set between a run and the stored task and
// events (section 13.8 R1), for the recovery check and for EndRun inside
// its write transaction: the binding equals the stored task's; an ended
// run's task_started says whether the task exists; a store_end has its
// event. It returns "" or what is wrong; err is a failed read.
func runVsStore(tx *sql.Tx, r Run, version int) (string, error) {
	var consumer, agent, wallet, payer string
	contract := contracts.Version
	q := `SELECT consumer_id, agent_id, wallet_id, payer, contract_version FROM tasks WHERE task_id = ?`
	if version < 3 {
		q = `SELECT consumer_id, agent_id, wallet_id, payer, 'apa.m1.v2' FROM tasks WHERE task_id = ?`
	}
	err := tx.QueryRow(q, r.TaskID).Scan(&consumer, &agent, &wallet, &payer, &contract)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if exists && (consumer != r.ConsumerID || agent != r.AgentID || wallet != r.WalletID || payer != r.Payer) {
		return "binding differs from the stored task", nil
	}
	if exists && contract != r.ContractVersion {
		return "contract version differs from the stored task's", nil
	}
	if r.DelegationID != "" {
		var owner string
		var task sql.NullString
		if err := tx.QueryRow(`SELECT consumer_id, task_id FROM delegations WHERE delegation_id = ?`, r.DelegationID).Scan(&owner, &task); errors.Is(err, sql.ErrNoRows) {
			return "the run's delegation does not exist", nil
		} else if err != nil {
			return "", err
		}
		// A run whose task exists bound the delegation to it; a run that made
		// no task (its Gate failed) left it to a later run.
		if owner != r.ConsumerID || exists && task.String != r.TaskID {
			return "the run's delegation is another consumer's, or not bound to the run's task", nil
		}
	}
	if r.State != "ended" {
		return "", nil
	}
	if r.TaskStarted != exists {
		return fmt.Sprintf("task_started is %t but the task %s", r.TaskStarted, map[bool]string{true: "exists", false: "does not exist"}[exists]), nil
	}
	if r.StoreEnd != "" {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM events WHERE task_id = ? AND event_type = ?`, r.TaskID, r.StoreEnd).Scan(&n); err != nil {
			return "", err
		}
		if n != 1 {
			return "store_end " + r.StoreEnd + " has no matching event", nil
		}
	}
	return "", nil
}

// RunByRequest finds the run of requestID with a read only query: no write
// lock, no clock check, so a resend is answered even when the clock is
// behind or the scenario has been retired (section 13.8 R3).
func (s *Store) RunByRequest(requestID string) (Run, bool, error) {
	w, err := scanRun(s.r.QueryRow(`SELECT `+runCols+` FROM host_runs WHERE request_id = ?`, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, &ReadError{Check: "host_runs", Err: err}
	}
	return w.r, true, nil
}

// AcceptRun is T1 (section 13.3): inside one write transaction it checks the
// request ID again (a concurrent twin may have won), allocates the first
// candidate task that is neither stored nor reserved, and inserts the row.
// created is false when the request ID was already there; nothing is
// written then. No event is written and no task is created.
func (s *Store) AcceptRun(req RunRequest) (run Run, created bool, err error) {
	if !requestIDPattern.MatchString(req.RequestID) || req.Scenario == "" || req.ScenarioDigest == "" {
		return Run{}, false, refuse("INVALID_RUN_RECORD", "request_id, scenario and digest are required")
	}
	version := cmp.Or(req.ContractVersion, contracts.Version)
	if paycontract.Supported(version) != (req.DelegationID != "") || version != contracts.Version && !paycontract.Supported(version) {
		return Run{}, false, refuse("INVALID_RUN_RECORD", "a pay-family run, and only one, starts with a delegation")
	}
	err = s.write(func(tx *sql.Tx, now uint64) error {
		w, err := scanRun(tx.QueryRow(`SELECT `+runCols+` FROM host_runs WHERE request_id = ?`, req.RequestID))
		if err == nil {
			run = w.r
			return nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var dlg any
		if req.DelegationID != "" { // confirmed, this consumer's, not bound, not expired, and no other open run holds it
			d, bound, err := readDelegation(tx, req.DelegationID)
			if err != nil {
				return err
			}
			var held int
			if err := tx.QueryRow(`SELECT count(*) FROM host_runs WHERE delegation_id = ? AND state = 'accepted'`, req.DelegationID).Scan(&held); err != nil {
				return err
			}
			if d == nil || d.ConsumerID != req.ConsumerID || bound || now >= d.ExpiresAt || held > 0 {
				return refuse("DELEGATION_UNAVAILABLE", "no unbound, unexpired delegation of this consumer that no other run holds")
			}
			dlg = req.DelegationID
		}
		for _, id := range req.Candidates {
			task, wallet, err := s.registry.Binding(id)
			if err != nil || task.ConsumerID != req.ConsumerID {
				continue
			}
			var used int
			if err := tx.QueryRow(`SELECT (SELECT count(*) FROM tasks WHERE task_id = ?) + (SELECT count(*) FROM host_runs WHERE task_id = ?)`, id, id).Scan(&used); err != nil {
				return err
			}
			if used > 0 {
				continue
			}
			b := make([]byte, 12)
			if _, err := rand.Read(b); err != nil {
				return err
			}
			run = Run{RunID: "run-" + hex.EncodeToString(b), RequestID: req.RequestID, Scenario: req.Scenario, ScenarioDigest: req.ScenarioDigest,
				BudgetInitial: req.BudgetInitial, TaskID: task.TaskID, ConsumerID: task.ConsumerID, AgentID: task.AgentID,
				WalletID: wallet.WalletID, Payer: wallet.From, State: "accepted", AcceptedAt: now, ContractVersion: version, DelegationID: req.DelegationID}
			_, err = tx.Exec(`INSERT INTO host_runs (run_id, request_id, scenario, scenario_digest, budget_initial, task_id, consumer_id, agent_id, wallet_id, payer, state, accepted_at, contract_version, delegation_id)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'accepted', ?, ?, ?)`, run.RunID, run.RequestID, run.Scenario, run.ScenarioDigest, run.BudgetInitial,
				run.TaskID, run.ConsumerID, run.AgentID, run.WalletID, run.Payer, now, version, dlg)
			created = err == nil
			return err
		}
		return refuse("NO_FREE_TASK", "every demo task of consumer "+req.ConsumerID+" is used or reserved")
	})
	return run, created, err
}

// EndRun is T4: an accepted run ends with its result. The result is checked
// with the recovery rules first (the row's own rules and runVsStore against
// the task and events, inside the write); nothing is written if it is not valid, and
// a run that is no longer accepted (ended, interrupted) is never rewritten.
func (s *Store) EndRun(runID string, e RunEnd) error {
	return s.write(func(tx *sql.Tx, now uint64) error {
		w, err := scanRun(tx.QueryRow(`SELECT `+runCols+` FROM host_runs WHERE run_id = ?`, runID))
		if errors.Is(err, sql.ErrNoRows) {
			return refuse("RUN_NOT_OPEN", "no run "+runID)
		} else if err != nil {
			return err
		}
		if w.r.State != "accepted" {
			return refuse("RUN_NOT_OPEN", "run "+runID+" is "+w.r.State)
		}
		started := int64(0)
		if e.TaskStarted {
			started = 1
		}
		w.r.State, w.r.EndedAt = "ended", now
		w.endedAt = sql.NullInt64{Int64: int64(now), Valid: true}
		w.r.Outcome, w.r.Detail, w.r.TaskStarted, w.r.StoreEnd, w.r.StoreEndError = e.Outcome, e.Detail, e.TaskStarted, e.StoreEnd, e.StoreEndError
		w.outcome, w.detail = sql.NullString{String: e.Outcome, Valid: true}, sql.NullString{String: e.Detail, Valid: true}
		w.taskStarted, w.storeEnd = sql.NullInt64{Int64: started, Valid: true}, sql.NullString{String: e.StoreEnd, Valid: true}
		w.storeEndError = sql.NullString{String: e.StoreEndError, Valid: e.StoreEndError != ""}
		if why := validRun(w); why != "" {
			return refuse("INVALID_RUN_RECORD", why)
		}
		// The same check against the stored task and events as on open, in
		// this transaction: a record the recovery would refuse is never written.
		if why, err := runVsStore(tx, w.r, schemaVersion); err != nil {
			return err
		} else if why != "" {
			return refuse("INVALID_RUN_RECORD", why)
		}
		res, err := tx.Exec(`UPDATE host_runs SET state = 'ended', ended_at = ?, outcome = ?, detail = ?, task_started = ?, store_end = ?, store_end_error = ?
			WHERE run_id = ? AND state = 'accepted'`, now, e.Outcome, e.Detail, started, e.StoreEnd, w.storeEndError, runID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil || n != 1 {
			return refuse("RUN_NOT_OPEN", "run "+runID+" changed meanwhile")
		}
		return nil
	})
}

// InterruptOpenRuns is the startup recovery R: every run still accepted
// belongs to a previous host process (one host per database, by its lock),
// so it is marked interrupted. No event is written and no task cancelled.
func (s *Store) InterruptOpenRuns() (n int64, err error) {
	err = s.write(func(tx *sql.Tx, now uint64) error {
		res, err := tx.Exec(`UPDATE host_runs SET state = 'interrupted', interrupted_at = ? WHERE state = 'accepted'`, now)
		if err == nil {
			n, err = res.RowsAffected()
		}
		return err
	})
	return n, err
}

// GetRun reads one run (read only).
func (s *Store) GetRun(runID string) (Run, bool, error) {
	w, err := scanRun(s.r.QueryRow(`SELECT `+runCols+` FROM host_runs WHERE run_id = ?`, runID))
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, false, nil
	}
	if err != nil {
		return Run{}, false, &ReadError{Check: "host_runs", Err: err}
	}
	return w.r, true, nil
}

// Runs lists the latest runs, newest first (read only).
func (s *Store) Runs(limit int) ([]Run, error) {
	rows, err := s.r.Query(`SELECT `+runCols+` FROM host_runs ORDER BY accepted_at DESC, rowid DESC LIMIT ?`, limit)
	if err != nil {
		return nil, &ReadError{Check: "host_runs", Err: err}
	}
	out := []Run{}
	for rows.Next() {
		w, err := scanRun(rows)
		if err != nil {
			rows.Close()
			return nil, &ReadError{Check: "host_runs", Err: err}
		}
		out = append(out, w.r)
	}
	if err := drained(rows); err != nil {
		return nil, &ReadError{Check: "host_runs", Err: err}
	}
	return out, nil
}

// TaskReserved says whether a run has reserved taskID (the --plan mode
// skips such tasks: section 13.8 R1).
func (s *Store) TaskReserved(taskID string) (bool, error) {
	var n int
	if err := s.r.QueryRow(`SELECT count(*) FROM host_runs WHERE task_id = ?`, taskID).Scan(&n); err != nil {
		return false, &ReadError{Check: "host_runs", Err: err}
	}
	return n > 0, nil
}
