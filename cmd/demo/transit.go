package main

// The subway pass (docs/commercial-transit.md, Aaron 11/9: "能夠啟動自動扣款"):
// the customer confirms a transit entry's delegation once; the host opens a
// task for it with no Agent and no process (POST /api/host/transit), and
// each time the customer walks in through a gate the page asks for one
// ride's fare (POST /api/host/transit/fare), which the host submits as the
// Agent's tools would and the Policy decides at once. An Allow is paid as
// any Allow is (payflow's Starter); the gate opens on the decision. A Deny
// ends the task (the pay contract's rule), so it ends the pass: the next
// gate asks the customer to open another. A pass is not a run's place (one
// run per consumer, --max-runs): the customer still shops while it is open.

import (
	"fmt"
	"net/http"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/toolbridge"
)

// transitFare is the product a ride pays (internal/merchant).
const transitFare = "subway-fare"

type transitPass struct {
	run        store.Run
	binding    *toolbridge.Binding
	capability string         // its bridge registration: never handed out, revoked when the pass ends
	rides      map[string]int // fare request ID -> ride number: a resend asks for the same ride
	next       int
}

// openTransit is POST /api/host/transit {request_id, scenario, delegation_id}:
// a transit entry's pass for its consumer, with that consumer's confirmed
// delegation. A resend answers with the pass; while a pass is open another
// is refused (transit_open); an ended one is closed and replaced.
func (c *control) openTransit(w http.ResponseWriter, r *http.Request, a *api) {
	var req struct {
		RequestID    string `json:"request_id"`
		Scenario     string `json:"scenario"`
		DelegationID string `json:"delegation_id"`
	}
	if !postGuard(w, r, a, &req) {
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || !scenarioID.MatchString(req.Scenario) || !paycontract.ValidID(req.DelegationID) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sc := c.scenarios[req.Scenario]
	switch {
	case sc == nil || !sc.Transit:
		fail(w, http.StatusBadRequest, "unknown_scenario")
		return
	case c.shop == nil || c.runner == nil || c.runner.cond == nil:
		fail(w, http.StatusServiceUnavailable, "transit_unavailable")
		return
	}
	if p := c.transit[sc.ConsumerID]; p != nil {
		if p.run.RequestID == req.RequestID {
			reply(w, http.StatusOK, c.view(p.run))
			return
		}
		if c.passOpen(p) {
			reply(w, http.StatusConflict, map[string]any{"error": "transit_open", "run": c.view(p.run)})
			return
		}
		c.closeTransit(p)
	}
	if run, found, ok := c.lookup(w, req.RequestID); !ok {
		return
	} else if found { // an earlier pass (ended, or interrupted by a restart): as it is
		c.answer(w, run, req.Scenario)
		return
	}
	run, _, err := c.st.AcceptRun(store.RunRequest{RequestID: req.RequestID, Scenario: sc.ID, ScenarioDigest: sc.digest,
		BudgetInitial: string(sc.BudgetInitial), ConsumerID: sc.ConsumerID, Candidates: sc.candidates,
		ContractVersion: sc.version(), DelegationID: req.DelegationID})
	switch {
	case store.Code(err) == "NO_FREE_TASK":
		fail(w, http.StatusConflict, "no_free_task")
		return
	case store.Code(err) == "DELEGATION_UNAVAILABLE":
		fail(w, http.StatusConflict, "delegation_unavailable")
		return
	case err != nil:
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "store_unavailable", "code": store.CodeOf(err)})
		return
	}
	// The task as a run's, with no Agent: the host proposes each fare.
	cond := *c.runner.cond
	cond.Agent = "none"
	task, wallet, err := c.runner.reg.Binding(run.TaskID)
	var setup store.TaskSetup
	if err == nil {
		setup, err = c.runner.taskSetup(run.TaskID, run.ConsumerID, planRun{BudgetInitial: sc.BudgetInitial, contract: run.ContractVersion, delegation: run.DelegationID}, &cond)
	}
	if err == nil {
		err = c.st.StartTask(setup)
	}
	if err != nil {
		code := store.CodeOf(err)
		c.endTransitRun(run, store.RunEnd{Outcome: "start_failed", Detail: "prepare_failed:" + code})
		reply(w, http.StatusConflict, map[string]string{"error": "transit_unavailable", "code": code})
		return
	}
	b := &toolbridge.Binding{Session: contracts.Session{ConsumerID: task.ConsumerID, AgentID: task.AgentID, TaskID: task.TaskID}, WalletID: wallet.WalletID, From: wallet.From, Scene: sc.Scene}
	capability, err := c.bridge.Register(b)
	if err != nil || !b.Arm() {
		c.bridge.Revoke(capability)
		c.cancelTransitTask(run)
		reply(w, http.StatusServiceUnavailable, map[string]string{"error": "transit_unavailable", "code": "STORE_ERROR"})
		return
	}
	p := &transitPass{run: run, binding: b, capability: capability, rides: map[string]int{}}
	c.transit[run.ConsumerID] = p
	c.wg.Go(func() { // the host's end closes the pass, before the Store closes
		<-c.ctx.Done()
		c.mu.Lock()
		if c.transit[run.ConsumerID] == p {
			c.closeTransit(p)
		}
		c.mu.Unlock()
	})
	fmt.Fprintf(c.out, "demo: transit pass %s task %s for %s opened\n", run.RunID, run.TaskID, run.ConsumerID)
	reply(w, http.StatusCreated, c.view(run))
}

// fare is POST /api/host/transit/fare {request_id, consumer_id, station}: one
// ride's fare on the consumer's open pass, decided now. The answer is the
// decision ("Allow" opens the gate) and whether the pass is still open.
func (c *control) fare(w http.ResponseWriter, r *http.Request, a *api) {
	var req struct {
		RequestID  string `json:"request_id"`
		ConsumerID string `json:"consumer_id"`
		Station    string `json:"station"`
	}
	if !postGuard(w, r, a, &req) {
		return
	}
	if !requestIDFormat.MatchString(req.RequestID) || !scenarioID.MatchString(req.ConsumerID) || !scenarioID.MatchString(req.Station) {
		fail(w, http.StatusBadRequest, "bad_request")
		return
	}
	c.mu.Lock()
	p := c.transit[req.ConsumerID]
	if p == nil {
		c.mu.Unlock()
		fail(w, http.StatusConflict, "no_transit_pass")
		return
	}
	ride, ok := p.rides[req.RequestID]
	if !ok {
		p.next++
		ride = p.next
		p.rides[req.RequestID] = ride
	}
	c.mu.Unlock()
	v, replay, code := c.shop.Fare(r.Context(), p.binding, transitFare, ride)
	c.mu.Lock()
	open := c.passOpen(p)
	if !open && c.transit[req.ConsumerID] == p {
		c.closeTransit(p)
	}
	c.mu.Unlock()
	pass := map[bool]string{true: "open", false: "closed"}[open]
	fmt.Fprintf(c.out, "demo: transit task %s ride %d at %s: decision=%q reason=%q code=%q pass=%s\n",
		p.run.TaskID, ride, req.Station, v.Decision.PolicyVerdict, v.Decision.Reason, code, pass)
	if code != "" {
		reply(w, http.StatusConflict, map[string]any{"error": "fare_refused", "code": code, "ride": ride, "pass": pass})
		return
	}
	reply(w, http.StatusOK, map[string]any{"ride": ride, "station": req.Station, "task_id": p.run.TaskID, "purchase_order_id": v.PurchaseOrderID,
		"amount": v.Amount, "decision": v.Decision.PolicyVerdict, "reason": v.Decision.Reason, "replay": replay, "pass": pass})
}

// passOpen: its task has not ended (a Deny, a cancellation). c.mu is held.
func (c *control) passOpen(p *transitPass) bool {
	ended, err := c.st.TaskEnded(p.run.TaskID)
	return err == nil && !ended
}

// closeTransit ends the pass: no more fares; its task cancelled unless a
// Deny ended it; its run recorded. Payments already asked for go on as
// payflow takes them. c.mu is held.
func (c *control) closeTransit(p *transitPass) {
	if c.transit[p.run.ConsumerID] == p {
		delete(c.transit, p.run.ConsumerID)
	}
	c.bridge.Revoke(p.capability)
	c.cancelTransitTask(p.run)
}

// cancelTransitTask ends a started pass's task and run.
func (c *control) cancelTransitTask(run store.Run) {
	end := store.RunEnd{Outcome: "ok", TaskStarted: true}
	if ended, err := c.st.TaskEnded(run.TaskID); err == nil && !ended {
		end.Outcome, end.StoreEnd = "cancelled", "TASK_CANCELLED"
		if err := c.st.Cancel(run.TaskID); err != nil {
			end.StoreEnd, end.StoreEndError = "", store.CodeOf(err)
		}
	}
	c.endTransitRun(run, end)
}

func (c *control) endTransitRun(run store.Run, end store.RunEnd) {
	if err := c.st.EndRun(run.RunID, end); err != nil {
		fmt.Fprintf(c.out, "demo: transit pass %s: its end was not recorded: %s\n", run.RunID, store.CodeOf(err))
		return
	}
	fmt.Fprintf(c.out, "demo: transit pass %s task %s closed: outcome=%q store_end=%q\n", run.RunID, run.TaskID, end.Outcome, end.StoreEnd)
}
