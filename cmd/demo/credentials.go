package main

// C2: the Host's side of apa.pay.v2's BBS+ credential (docs/commercial-c2-design.md
// 5 to 7). The test issuer, the holders and the verifier all run in this
// process: this shows the format, the bindings, revocation and the
// decision's wiring, not that the three are independent.

import (
	"context"
	"fmt"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
)

// verifierID names this Host in every presentation's nonce.
const verifierID = "apa-demo-host"

// statusRefresh is how often the running Host re-signs the status list
// (C2-D4 (a)); its freshness window is at most credential.MaxStatusWindow.
const statusRefresh = 300 * time.Second

// refreshStatus re-signs the status list every period until ctx ends,
// through the same locked writer as apa-issuer revoke, so a revocation is
// never overwritten. A failure is reported by a fixed text and retried at
// the next period; verifications meanwhile read the list as it is.
func refreshStatus(ctx context.Context, d *credential.Dir, every time.Duration, deps deps) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := d.Refresh(int64(deps.clock())); err != nil {
				fmt.Fprintln(deps.out, "demo: status list refresh failed")
			}
		}
	}
}

// taskCredential is an apa.pay.v2 task's credential, issued before
// StartTask for 24 h of host time (C2-D10), or declared absent when the
// consumer has no registered holder (design 5).
func taskCredential(d *credential.Dir, setup *store.TaskSetup, consumer string, now uint64) error {
	if d == nil {
		return fmt.Errorf("an apa.pay.v2 task needs the credential directory")
	}
	issued, ok, err := d.IssueTask(setup.TaskID, consumer, int64(now+store.MaxCredentialTTL))
	switch {
	case err != nil:
		return err
	case !ok:
		setup.CredentialAbsent = true
		return nil
	}
	setup.BBS, err = store.NewBBSCredential(d, issued)
	return err
}
