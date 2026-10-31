package store

// apa.pay.v2's BBS+ credential in the Store (C2b, docs/commercial-c2-design.md
// 3.3, 5, 7 and 8). The Store holds no issuer or holder key: the Host issues
// the credential before StartTask and presents it inside Commit; the Store
// keeps the task's snapshot, the challenges and the verification records,
// and verifies under its write transaction.

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/credential"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/paycontract"
)

// MaxCredentialTTL caps a credential's expiry after its task starts (C2-D10).
const MaxCredentialTTL = 24 * 3600

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BBSCredential is an apa.pay.v2 task's credential_bbs snapshot (design 5):
// the trust settings and the issued credential's fields, fixed when the task
// starts; files changed later do not change it.
type BBSCredential struct {
	Schema          string `json:"schema"`
	Issuer          string `json:"issuer"`
	IssuerPK        string `json:"issuer_pk"`         // hex
	StatusKeySHA256 string `json:"status_key_sha256"` // the status list's key, credential.StatusKeyFingerprint
	ConsumerID      string `json:"consumer_id"`
	Holder          string `json:"holder"` // the consumer's registered holder field
	TaskID          string `json:"task_id"`
	Expiry          uint64 `json:"expiry"`
	Status          string `json:"status"`           // the status list index
	SignatureSHA256 string `json:"signature_sha256"` // the issued signature's, for tracing only
}

// validate checks every field's spelling, by the envelope's own rules.
func (b BBSCredential) validate() error {
	// v1's fields spelled as v1's (the snapshot keeps no v2 attribute: a venue asks for those at the purchase).
	c := credential.Credential{"schema": credential.Schema, "issuer": b.Issuer, "task_id": b.TaskID, "consumer_id": b.ConsumerID, "c0": credential.C0Placeholder,
		"expiry": strconv.FormatUint(b.Expiry, 10), "holder": b.Holder, "status": b.Status}
	pk, err := hex.DecodeString(b.IssuerPK)
	switch {
	case b.Schema != credential.Schema && b.Schema != credential.SchemaV2:
		return errors.New("credential_bbs: schema")
	case c.Check() != nil:
		return c.Check()
	case err != nil || len(pk) == 0 || hex.EncodeToString(pk) != b.IssuerPK:
		return errors.New("credential_bbs: issuer_pk is lowercase hex")
	case !sha256Hex.MatchString(b.StatusKeySHA256) || !sha256Hex.MatchString(b.SignatureSHA256):
		return errors.New("credential_bbs: status_key_sha256 and signature_sha256 are SHA-256 in hex")
	}
	return nil
}

// bbs is an apa.pay.v2 task's stored credential snapshot; nil when the task
// started without one (absent).
func (t *taskTx) bbs() (*BBSCredential, error) {
	if !t.row.CredentialSnap.Valid {
		return nil, nil
	}
	b := new(BBSCredential)
	return b, decodeSnapshot(t.tx, t.row.CredentialSnap.String, "credential_bbs", b)
}

// NewBBSCredential is the snapshot of a credential issued from dir
// (credential.Dir.IssueTask), with dir's trust settings as they are now.
func NewBBSCredential(dir *credential.Dir, s credential.Stored) (*BBSCredential, error) {
	issuer, pk, err := dir.IssuerPublic()
	if err != nil {
		return nil, err
	}
	fp, err := dir.StatusKeyFingerprint()
	if err != nil {
		return nil, err
	}
	c := s.Credential
	expiry, err := strconv.ParseUint(c["expiry"], 10, 64)
	if err != nil || c["issuer"] != issuer {
		return nil, errors.New("credential_bbs: the credential is not this directory's issuer's")
	}
	b := &BBSCredential{Schema: c["schema"], Issuer: issuer, IssuerPK: hex.EncodeToString(pk), StatusKeySHA256: fp, ConsumerID: c["consumer_id"],
		Holder: c["holder"], TaskID: c["task_id"], Expiry: expiry, Status: c["status"], SignatureSHA256: credential.Fingerprint(s.Signature)}
	return b, b.validate()
}

// ChallengeSeconds is how long an issued challenge stays usable (design 7).
const ChallengeSeconds = 60

// ChallengeReuseMargin: a challenge with less than this left is not reused,
// so a retry is not denied CHALLENGE_EXPIRED by a clock tick (review note 3).
const ChallengeReuseMargin = 5

// Challenge is a stored presentation request for one (task, purchase) and
// intent (design 7 step 4).
type Challenge struct {
	ID, TaskID, PurchaseOrderID, IntentHash, VerifierID string
	Disclose                                            []string
	IssuedAt, ExpiresAt                                 uint64
	Schema                                              string // the task's credential's (its snapshot's)
}

// Request is what the holder answers and the verifier checks.
func (c Challenge) Request() credential.Request {
	return credential.Request{VerifierID: c.VerifierID, ChallengeID: c.ID, IntentHash: c.IntentHash, Disclose: c.Disclose, Schema: c.Schema}
}

// Presented is the Host holder's answer to a challenge: a presentation, or
// Unavailable when the holder material is missing, corrupt or not the
// registered key (design 7 step 3: a decided Deny). A proof that failed to
// build is not presented at all (C2-D12): no submit, no decision.
// Require is what the product's venue asks of the subject (docs/commercial-venues.md 3.2): attribute ->
// the value it must have ("" any); the Host's, from the shop's catalog, as the challenge's Ask was.
type Presented struct {
	Presentation *credential.Presentation
	Unavailable  bool
	Require      map[string]string
}

// payTask opens sess's task for a pay-family order of po's version: the
// session owns it, and the order's version is the task's, which is version.
func (s *Store) payTask(tx *sql.Tx, now uint64, sess contracts.Session, po paycontract.PurchaseOrder, version string) (*taskTx, error) {
	t, err := s.openTask(tx, now, sess.TaskID)
	if Code(err) == "UNKNOWN_TASK" || err == nil && (t.row.ConsumerID != sess.ConsumerID || t.row.AgentID != sess.AgentID) {
		return nil, refuse("OWNERSHIP_MISMATCH", "session does not own this task")
	} else if err != nil {
		return nil, err
	}
	if t.row.ContractVersion != version || po.Version != version {
		return nil, refuse("CONTRACT_VERSION_MISMATCH", "the purchase's contract is not the task's ("+t.row.ContractVersion+")")
	}
	return t, nil
}

func decodePay(raw []byte) (paycontract.PurchaseOrder, error) {
	var po paycontract.PurchaseOrder
	if err := contracts.Decode(raw, &po); err != nil {
		return po, refuse("INVALID_PURCHASE", "purchase does not decode as a pay-family purchase order")
	}
	return po, nil
}

// IssueChallenge returns the challenge the Host's holder answers for an
// apa.pay.v2 purchase, after the same checks as SubmitV2's (design 7 steps
// 1, 2, 4). ok is false when there is nothing to present: the purchase is
// already stored (SubmitV2 returns it), or the task's credential is absent.
// An issued challenge of the same intent with ChallengeReuseMargin left is reused;
// any other issued one of this purchase is marked expired first.
// ask: the attributes the product's venue asks for (docs/commercial-venues.md 3.2), disclosed besides the
// mandatory fields when the task's credential is v2; a v1 credential has none to disclose.
func (s *Store) IssueChallenge(sess contracts.Session, raw []byte, ask []string) (Challenge, bool, error) {
	po, err := decodePay(raw)
	if err != nil {
		return Challenge{}, false, err
	}
	var out Challenge
	var ok bool
	err = s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.payTask(tx, now, sess, po, paycontract.V2)
		if err != nil {
			return err
		}
		if _, _, replay, err := s.payChecks(t, sess, po); err != nil {
			return err
		} else if replay != nil {
			return t.finish() // an Ask that expired meanwhile is recorded, as Submit's replay does
		}
		b, err := t.bbs()
		if err != nil {
			return err
		} else if b == nil {
			return t.finish()
		}
		intent, err := po.IntentHash()
		if err != nil {
			return contractRefusal(err)
		}
		poID := po.Intent.PurchaseOrderID
		if _, err := tx.Exec(`UPDATE credential_challenges SET state = 'expired' WHERE task_id = ? AND purchase_order_id = ? AND state = 'issued'
			AND (expires_at <= ? OR intent_hash != ?)`, t.row.TaskID, poID, now+ChallengeReuseMargin, intent); err != nil {
			return err
		}
		out = Challenge{TaskID: t.row.TaskID, PurchaseOrderID: poID, Schema: b.Schema}
		var disclose string
		err = tx.QueryRow(`SELECT challenge_id, intent_hash, disclose, verifier_id, issued_at, expires_at FROM credential_challenges
			WHERE task_id = ? AND purchase_order_id = ? AND state = 'issued'`, t.row.TaskID, poID).
			Scan(&out.ID, &out.IntentHash, &disclose, &out.VerifierID, &out.IssuedAt, &out.ExpiresAt)
		switch {
		case err == nil:
			ok = true
			return errors.Join(json.Unmarshal([]byte(disclose), &out.Disclose), t.finish())
		case !errors.Is(err, sql.ErrNoRows):
			return err
		}
		rnd := make([]byte, 16)
		if _, err := rand.Read(rnd); err != nil {
			return err
		}
		out.ID, out.IntentHash, out.VerifierID, out.Disclose = "chl-"+hex.EncodeToString(rnd), intent, s.verifier, credential.Mandatory
		if b.Schema == credential.SchemaV2 {
			for _, a := range credential.Attributes { // in their order; an unknown name is never asked
				if slices.Contains(ask, a) {
					out.Disclose = append(slices.Clone(out.Disclose), a)
				}
			}
		}
		out.IssuedAt, out.ExpiresAt = now, now+ChallengeSeconds
		d, err := canonical(out.Disclose)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO credential_challenges VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'issued')`,
			out.ID, out.TaskID, poID, intent, string(d), out.VerifierID, out.IssuedAt, out.ExpiresAt); err != nil {
			return err
		}
		ok = true
		return t.finish()
	})
	return out, ok, err
}

// SubmitV2 is Submit for an apa.pay.v2 task: the same checks, then the
// credential step with the Host holder's answer, the decision and the
// write, in one transaction (design 7 step 6). A stored purchase is
// returned as it is, whatever was presented.
func (s *Store) SubmitV2(sess contracts.Session, raw []byte, pr Presented) (Result, error) {
	po, err := decodePay(raw)
	if err != nil {
		return Result{}, err
	}
	var out Result
	err = s.write(func(tx *sql.Tx, now uint64) error {
		t, err := s.payTask(tx, now, sess, po, paycontract.V2)
		if err != nil {
			return err
		}
		return s.submitPay(t, sess, po, &pr, &out)
	})
	return out, err
}

// verifyCredential is the credential step (design 3.3, 4, 7): it consumes
// the challenge the Store keeps for this purchase, whatever the
// presentation names, and verifies against the task's snapshot and the
// status list read now. ponytail: verifies inside the write transaction
// (about 3.4 ms); move it before the transaction if that becomes the
// bottleneck (C1 review N5).
func (s *Store) verifyCredential(t *taskTx, po paycontract.PurchaseOrder, pr Presented) (*contracts.CredentialCheck, error) {
	c := &contracts.CredentialCheck{Verifier: s.verifier}
	b, err := t.bbs()
	switch {
	case err != nil:
		return nil, err
	case b == nil:
		c.Result = credential.Absent
		return c, nil
	case pr.Unavailable:
		c.Result, c.Detail = credential.Invalid, "HOLDER_MATERIAL_UNAVAILABLE"
		return c, nil
	case pr.Presentation == nil:
		return nil, refuse("PRESENTATION_REQUIRED", "the task has a credential: present it, or report the holder material unavailable")
	}
	poID := po.Intent.PurchaseOrderID
	var ch Challenge
	var disclose string
	err = t.tx.QueryRow(`SELECT challenge_id, intent_hash, disclose, verifier_id, expires_at FROM credential_challenges
		WHERE task_id = ? AND purchase_order_id = ? AND state = 'issued'`, t.row.TaskID, poID).Scan(&ch.ID, &ch.IntentHash, &disclose, &ch.VerifierID, &ch.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		c.Result, c.Detail = credential.Invalid, "CHALLENGE_MISSING"
		return c, nil
	} else if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(disclose), &ch.Disclose); err != nil {
		return nil, err
	}
	ch.Schema = b.Schema
	if _, err := t.tx.Exec(`UPDATE credential_challenges SET state = 'used' WHERE challenge_id = ?`, ch.ID); err != nil {
		return nil, err
	}
	c.ChallengeID = ch.ID
	intent, err := po.IntentHash()
	if err != nil {
		return nil, contractRefusal(err)
	}
	switch {
	case t.now >= ch.ExpiresAt:
		c.Result, c.Detail = credential.Invalid, "CHALLENGE_EXPIRED"
	case pr.Presentation.ChallengeID != ch.ID:
		c.Result, c.Detail = credential.Invalid, "CHALLENGE_MISMATCH"
	case ch.IntentHash != intent:
		c.Result, c.Detail = credential.Invalid, "INTENT_MISMATCH"
	}
	if c.Result != "" {
		return c, nil
	}
	st, rollback, err := s.status(t, b)
	if err != nil {
		return nil, err
	}
	pk, _ := hex.DecodeString(b.IssuerPK)
	o := credential.Verify(ch.Request(), *pr.Presentation, credential.Expect{IssuerID: b.Issuer, IssuerPK: pk, Holder: b.Holder, TaskID: b.TaskID,
		Expiry: strconv.FormatUint(b.Expiry, 10), Status: b.Status}, st, int64(t.now))
	c.Result, c.Detail = o.Result, o.Detail
	if rollback && o.Result == credential.StatusUnavailable {
		c.Detail = "STATUS_LIST_ROLLBACK"
	}
	if o.Issuer != "" {
		c.CredentialSchema, c.Issuer, c.Holder, c.StatusIndex, c.Expiry, c.ProofSHA256, c.Claims = b.Schema, o.Issuer, o.Holder, o.Status, uint64(o.Expiry), o.ProofSHA256, o.Claims
	}
	if c.Result == credential.Valid && !meets(o.Claims, pr.Require) {
		c.Detail = "CLAIM_UNMET" // the credential is valid; it does not say what the venue asks (Deny CREDENTIAL_CLAIM_UNMET)
	}
	c.StatusListVersion, c.StatusListIssuedAt, c.StatusListSHA256 = o.StatusListVersion, uint64(o.StatusListIssuedAt), o.StatusListSHA256
	return c, nil
}

// meets: the disclosed claims ("name=value,…") say each required attribute, with its value when one is required.
func meets(claims string, require map[string]string) bool {
	got := map[string]string{}
	for _, kv := range strings.Split(claims, ",") {
		if k, v, ok := strings.Cut(kv, "="); ok {
			got[k] = v
		}
	}
	for a, want := range require {
		if v, ok := got[a]; !ok || want != "" && v != want {
			return false
		}
	}
	return true
}

var errNoDirectory = errors.New("status list: no credential directory")

var errRollback = errors.New("status list: an older version, or another list of the same version, than one already seen")

// status reads the task issuer's status list now, never cached (design 6),
// and records its version in credential_status_seen. A list older than one
// seen, or another of the same version, is not used: rollback.
func (s *Store) status(t *taskTx, b *BBSCredential) (credential.Status, bool, error) {
	if s.creds == nil { // reopened without --credential-dir: the list cannot be read (review S1)
		return credential.Status{Err: errNoDirectory}, false, nil
	}
	list, sum, err := s.creds.ReadStatus(b.Issuer, b.StatusKeySHA256, int64(t.now))
	if err != nil {
		return credential.Status{Err: err}, false, nil
	}
	var max uint64
	var seen string
	err = t.tx.QueryRow(`SELECT max_list_version, sha256 FROM credential_status_seen WHERE issuer = ?`, b.Issuer).Scan(&max, &seen)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = t.tx.Exec(`INSERT INTO credential_status_seen VALUES (?, ?, ?)`, b.Issuer, list.ListVersion, sum)
	case err != nil:
	case list.ListVersion < max || list.ListVersion == max && sum != seen:
		return credential.Status{Err: errRollback}, true, nil
	case list.ListVersion > max:
		_, err = t.tx.Exec(`UPDATE credential_status_seen SET max_list_version = ?, sha256 = ? WHERE issuer = ?`, list.ListVersion, sum, b.Issuer)
	}
	return credential.Status{List: list, SHA256: sum}, false, err
}

// recordVerification writes the purchase's verification record.
func (t *taskTx) recordVerification(poID string, c contracts.CredentialCheck) error {
	var chl any
	if c.ChallengeID != "" {
		chl = c.ChallengeID
	}
	_, err := t.tx.Exec(`INSERT INTO credential_verifications VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.row.TaskID, poID, chl, c.Result, c.Detail, c.Issuer, c.Holder, c.StatusIndex, c.Expiry,
		c.StatusListVersion, c.StatusListIssuedAt, c.StatusListSHA256, c.ProofSHA256, c.Verifier, t.now)
	return err
}

// intentExempt are the details whose record names a challenge of another
// intent, or none was compared (design 3.3).
var intentExempt = []string{"INTENT_MISMATCH", "CHALLENGE_EXPIRED", "CHALLENGE_MISMATCH"}

// verifyCredentials is design 3.3's cross-check of an apa.pay.v2 task:
// each record equals its CREDENTIAL_VERIFIED event, was made at its
// decision's time, agrees with the task's snapshot (absent exactly when
// the task has none), and names a used challenge of its own purchase (of
// its intent, but for the exempt details). It returns why not, or "".
func (t *taskTx) verifyCredentials() string {
	if t.row.ContractVersion != paycontract.V2 {
		return ""
	}
	b, err := t.bbs()
	if err != nil {
		return "credential snapshot: " + err.Error()
	}
	events := map[string]contracts.CredentialCheck{}
	for _, e := range t.events {
		if e.EventType == "CREDENTIAL_VERIFIED" {
			events[e.Scope.PurchaseOrderID] = *e.Payload.Credential
		}
	}
	rows, err := t.tx.Query(`SELECT purchase_order_id, coalesce(challenge_id, ''), result, detail, issuer, holder, status_index, expiry,
		status_list_version, status_list_issued_at, status_list_sha256, proof_sha256, verifier, verified_at FROM credential_verifications WHERE task_id = ?`, t.row.TaskID)
	if err != nil {
		return "credential records: " + err.Error()
	}
	defer rows.Close()
	for rows.Next() {
		var po string
		var c contracts.CredentialCheck
		var at uint64
		if err := rows.Scan(&po, &c.ChallengeID, &c.Result, &c.Detail, &c.Issuer, &c.Holder, &c.StatusIndex, &c.Expiry,
			&c.StatusListVersion, &c.StatusListIssuedAt, &c.StatusListSHA256, &c.ProofSHA256, &c.Verifier, &at); err != nil {
			return "credential records: " + err.Error()
		}
		e, ok := events[po]
		if c.Issuer != "" {
			c.CredentialSchema = credential.Schema
			if b != nil {
				c.CredentialSchema = b.Schema
			}
		}
		c.Claims = e.Claims // the event carries the claims; the record keeps the result and its detail (no schema change)
		p := t.purchases[po]
		switch {
		case !ok || e != c:
			return "purchase " + po + ": the credential record is not its CREDENTIAL_VERIFIED event"
		case p == nil || at != p.DecidedAt:
			return "purchase " + po + ": the credential record is not of its decision's time"
		case (b == nil) != (c.Result == credential.Absent):
			return "purchase " + po + ": a credential is absent exactly when the task has none"
		case c.Issuer != "" && (c.Issuer != b.Issuer || c.Holder != b.Holder || c.StatusIndex != b.Status || c.Expiry != b.Expiry):
			return "purchase " + po + ": the credential record is not the task's snapshot"
		}
		if c.ChallengeID != "" {
			var task, cpo, intent, state string
			err := t.tx.QueryRow(`SELECT task_id, purchase_order_id, intent_hash, state FROM credential_challenges WHERE challenge_id = ?`, c.ChallengeID).
				Scan(&task, &cpo, &intent, &state)
			if err != nil || task != t.row.TaskID || cpo != po || state != "used" || intent != p.Decision.IntentHash && !slices.Contains(intentExempt, c.Detail) {
				return "purchase " + po + ": the credential record's challenge is not a used one of this purchase and intent"
			}
		}
		delete(events, po)
	}
	if err := rows.Err(); err != nil {
		return "credential records: " + err.Error()
	}
	if len(events) > 0 {
		return "CREDENTIAL_VERIFIED without a credential record"
	}
	return ""
}

// verifyCredentialTables is schema 6's whole-table check: challenges and
// records belong to apa.pay.v2 tasks; a challenge is used exactly when a
// record names it (an issued one left by a failed presentation is fine);
// no record read a list version beyond the highest seen.
func verifyCredentialTables(tx *sql.Tx) error {
	for _, c := range []struct{ q, why string }{
		{`SELECT count(*) FROM credential_challenges c LEFT JOIN tasks t USING (task_id) WHERE t.contract_version IS NOT 'apa.pay.v2'
			OR (c.state = 'used') != EXISTS (SELECT 1 FROM credential_verifications v WHERE v.challenge_id = c.challenge_id)`,
			"a challenge is not an apa.pay.v2 task's, or is used without a record (or the reverse)"},
		{`SELECT count(*) FROM credential_verifications v LEFT JOIN tasks t USING (task_id) WHERE t.contract_version IS NOT 'apa.pay.v2'`,
			"a credential record is not an apa.pay.v2 task's"},
		{`SELECT count(*) FROM credential_verifications v LEFT JOIN credential_status_seen s USING (issuer)
			WHERE v.status_list_version > 0 AND (s.issuer IS NULL OR v.status_list_version > s.max_list_version)`,
			"a credential record read a status list newer than any seen"},
	} {
		var n int
		if err := tx.QueryRow(c.q).Scan(&n); err != nil {
			return readErr("credential", "", err)
		}
		if n > 0 {
			return &CorruptError{Check: "credential", Detail: c.why}
		}
	}
	return nil
}

// recheck is design 7.1's check of an apa.pay.v2 task's credential when an
// approval lands or a payment starts: the status list read now, then
// revocation, then expiry, in verification's order. It returns the
// credential reason that stops it, or "". An absent credential never
// reaches it (its purchases are denied).
func (s *Store) recheck(t *taskTx) (string, error) {
	if t.row.ContractVersion != paycontract.V2 {
		return "", nil
	}
	b, err := t.bbs()
	if err != nil || b == nil {
		return "", err
	}
	st, _, err := s.status(t, b)
	switch {
	case err != nil:
		return "", err
	case st.Err != nil:
		return "CREDENTIAL_STATUS_UNAVAILABLE", nil
	case slices.Contains(st.List.Revoked, b.Status):
		return "CREDENTIAL_REVOKED", nil
	case t.now >= b.Expiry:
		return "AUTHORIZATION_EXPIRED", nil
	}
	return "", nil
}
