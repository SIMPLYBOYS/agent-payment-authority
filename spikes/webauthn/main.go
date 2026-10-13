// a1-spike: the A1 WebAuthn feasibility spike (docs/commercial-human-approval-design.md
// 2, 4; decisions D1-D7 of section 17). It answers one question: on this Mac,
// which browser and authenticator can register and then approve one synthetic
// purchase at rp_id=localhost, and what a wrong challenge, a replay, a wrong
// origin and a request without UV actually do.
//
// Test use only. Nothing here is product code (A2 rebuilds it in the Signer):
// no payment, no keys, no Signer, no Agent, no Store. The purchase is a fixed
// synthetic one; state lives in memory and ends with the process. Results go
// to an append-only JSONL file, each line labelled with the authenticator
// source (real-device or virtual) so the two are never mixed.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

//go:embed page.html
var page []byte

const (
	rpID          = "localhost"
	challengeTTL  = 120 * time.Second
	challengeTag  = "agent-payment-authority/pay/v1/webauthn-challenge"
	consumerID    = "consumer-test"
	walletID      = "wallet-test"
	approverID    = "approver-test"
	configVersion = "a1-spike-v1"
)

// intent is the one synthetic purchase; intentHash is SHA-256 of its JSON.
// ponytail: fixed struct order is the canonical form here; A2 uses
// contracts.Canonical and the real v1 order.
var intent = struct {
	Guest   string `json:"guest"`
	Item    string `json:"item"`
	Amount  string `json:"amount"`
	PayTo   string `json:"pay_to"`
	Network string `json:"network"`
	Note    string `json:"note"`
}{"測試客人（合成）", "合成商品 A1-spike", "0.50 USDC", "0x0000000000000000000000000000000000000a11", "base-sepolia（不連線）",
	"A1 spike 測試用；不付款、不簽交易、不連鏈"}

var intentHash = func() string { b, _ := json.Marshal(intent); h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }()

// record is what a challenge is derived from (design 4.5, simplified: the
// session is one of two synthetic ones, jti is derived from it).
type record struct {
	Version          int    `json:"version"`
	ChallengeID      string `json:"challenge_id"`
	Random           string `json:"random"`
	SessionID        string `json:"approval_session_id"`
	Decision         string `json:"decision"`
	ConsumerID       string `json:"consumer_id"`
	WalletID         string `json:"wallet_id"`
	IntentHash       string `json:"intent_hash"`
	JTI              string `json:"jti"`
	ApproverID       string `json:"approver_id"`
	ApproverVersion  int    `json:"approver_version"`
	ConfigVersion    string `json:"config_version"`
	UserVerification string `json:"user_verification"`
	ExpiresAt        int64  `json:"expires_at"`
}

func (r record) challenge() []byte {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(append(append([]byte(challengeTag), 0), b...))
	return h[:]
}

type pending struct {
	rec     record
	session webauthn.SessionData
	used    bool
}

type user struct {
	id    []byte
	creds []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return u.id }
func (u *user) WebAuthnName() string                       { return "a1-spike-tester" }
func (u *user) WebAuthnDisplayName() string                { return "A1 spike tester" }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

type spike struct {
	mu      sync.Mutex // ponytail: one lock, one tester
	wa      *webauthn.WebAuthn
	hosts   map[string]bool // accepted Host and Origin values
	label   string          // authenticator source written on every result
	now     func() time.Time
	out     io.Writer
	user    user
	reg     *webauthn.SessionData
	ch      map[string]*pending
	suspect map[string]bool
}

func newSpike(port, label string, out io.Writer, now func() time.Time) (*spike, error) {
	origin := "http://localhost:" + port
	wa, err := webauthn.New(&webauthn.Config{
		RPID: rpID, RPDisplayName: "APA A1 spike（測試用）", RPOrigins: []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired, ResidentKey: protocol.ResidentKeyRequirementDiscouraged},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Timeout: challengeTTL, TimeoutUVD: challengeTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: challengeTTL, TimeoutUVD: challengeTTL}},
	})
	if err != nil {
		return nil, err
	}
	id := make([]byte, 32)
	rand.Read(id)
	return &spike{wa: wa, label: label, now: now, out: out, user: user{id: id},
		hosts: map[string]bool{"localhost:" + port: true, "127.0.0.1:" + port: true},
		ch:    map[string]*pending{}, suspect: map[string]bool{}}, nil
}

// result is one JSONL line. The credential ID is only kept as a short hash.
type result struct {
	Time          string   `json:"time"`
	Authenticator string   `json:"authenticator"`
	UserAgent     string   `json:"user_agent"`
	Origin        string   `json:"origin"`
	Step          string   `json:"step"`
	OK            bool     `json:"ok"`
	Code          string   `json:"code,omitempty"`
	Detail        string   `json:"detail,omitempty"`
	Session       string   `json:"session,omitempty"`
	ChallengeID   string   `json:"challenge_id,omitempty"`
	UVRequested   string   `json:"uv_requested,omitempty"`
	Flags         string   `json:"flags,omitempty"`
	SignCount     *uint32  `json:"sign_count,omitempty"`
	Format        string   `json:"fmt,omitempty"`
	AAGUID        string   `json:"aaguid,omitempty"`
	Attachment    string   `json:"attachment,omitempty"`
	Transports    []string `json:"transports,omitempty"`
	Credential    string   `json:"credential,omitempty"`
}

func flagString(f protocol.AuthenticatorFlags) string {
	b := func(on bool) string {
		if on {
			return "1"
		}
		return "0"
	}
	return "UP=" + b(f.UserPresent()) + " UV=" + b(f.UserVerified()) + " BE=" + b(f.HasBackupEligible()) + " BS=" + b(f.HasBackupState())
}

func credTag(id []byte) string { h := sha256.Sum256(id); return hex.EncodeToString(h[:4]) }

type apiError struct {
	status int
	code   string
	detail string
}

func (e *apiError) Error() string { return e.code }

func fail(status int, code, detail string) *apiError { return &apiError{status, code, detail} }

// Evidence keeps fixed words only (review R2 of 20677b0): the library's own
// text can carry the expected and received challenge (DevInfo, Info, some
// Details), so a library error is reduced to one of these reasons and its
// text never reaches the results file or an HTTP reply.
var reasonByDetails = map[string]string{
	"Error validating challenge":                                                   "challenge_mismatch",
	"Error validating origin":                                                      "origin_mismatch",
	"Error validating cross origin flag":                                           "cross_origin",
	"Error validating topOrigin":                                                   "top_origin",
	"Error validating top origin":                                                  "top_origin",
	"Error validating ceremony type":                                               "ceremony_type",
	"Session has Expired":                                                          "session_expired",
	"User handle and User ID do not match":                                         "user_handle_mismatch",
	"The credential ID provided is not owned by the user":                          "credential_not_allowed",
	"The credential ID provided is not in the sessions allowed credential list":    "credential_not_allowed",
	"Unable to find the credential for the returned credential ID":                 "credential_not_allowed",
	"Backup Eligible flag inconsistency detected during login validation":          "backup_eligible_changed",
	"Backup State Flag is true but Backup Eligible flag is false which is invalid": "backup_state_invalid",
}

var reasonByInfo = map[string]string{
	"User verification required but flag not set by authenticator": "uv_missing",
	"User presence required but flag not set by authenticator":     "up_missing",
}

// errorTypes are the library's fixed error type names (protocol/errors.go).
var errorTypes = map[string]bool{"invalid_request": true, "policy_restriction": true, "challenge_mismatch": true, "parse_error": true,
	"auth_data": true, "verification_error": true, "attestation_error": true, "invalid_attestation": true, "invalid_metadata": true,
	"invalid_certificate": true, "invalid_signature": true, "invalid_key_type": true, "unsupported_key_algorithm": true,
	"spec_unimplemented": true, "not_implemented": true}

func reason(err error) string {
	var pe *protocol.Error
	if !errors.As(err, &pe) {
		return "other"
	}
	if r, ok := reasonByDetails[pe.Details]; ok {
		return r
	}
	if r, ok := reasonByInfo[pe.DevInfo]; ok {
		return r
	}
	if strings.HasPrefix(pe.DevInfo, "RP Hash mismatch") {
		return "rp_id_mismatch"
	}
	if errorTypes[pe.Type] {
		return "other:" + pe.Type
	}
	return "other"
}

// allowed returns v when it is one of the known values, else "other".
func allowed(v string, known ...string) string {
	if v == "" {
		return ""
	}
	for _, k := range known {
		if v == k {
			return v
		}
	}
	return "other"
}

var steps = map[string]bool{"register": true, "approve": true, "replay": true, "wrong-challenge": true, "uv-discouraged": true}

// browserErrors are the DOMException names a WebAuthn call can raise, plus
// the page's own two; the message itself is never kept.
var browserErrors = []string{"NotAllowedError", "SecurityError", "InvalidStateError", "NotSupportedError", "AbortError",
	"ConstraintError", "UnknownError", "TypeError", "NetworkError", "Unsupported", "NotReady"}

func (s *spike) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.hosts[r.Host] {
		http.Error(w, "unknown host", http.StatusMisdirectedRequest)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(page)
		return
	}
	o := r.Header.Get("Origin")
	if r.Method != http.MethodPost || len(o) < 7 || !s.hosts[o[7:]] || o[:7] != "http://" || r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	res := result{Time: s.now().UTC().Format(time.RFC3339Nano), Authenticator: s.label, UserAgent: truncate(r.UserAgent(), 300), Origin: o}
	s.mu.Lock()
	reply, aerr := s.route(r.URL.Path, body, &res)
	s.mu.Unlock()
	if aerr != nil {
		res.Code, res.Detail = aerr.code, aerr.detail
	} else {
		res.OK = true
	}
	if res.Step != "" {
		line, _ := json.Marshal(res)
		if _, err := s.out.Write(append(line, '\n')); err != nil {
			log.Printf("a1-spike: result not saved: %v", err)
			aerr, reply = fail(http.StatusInternalServerError, "RESULT_NOT_SAVED", ""), nil
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if aerr != nil {
		w.WriteHeader(aerr.status)
		json.NewEncoder(w).Encode(map[string]string{"code": aerr.code, "detail": aerr.detail})
		return
	}
	json.NewEncoder(w).Encode(reply)
}

func (s *spike) route(path string, body []byte, res *result) (any, *apiError) {
	switch path {
	case "/api/register/begin":
		return s.registerBegin()
	case "/api/register/finish":
		res.Step = "register"
		return s.registerFinish(body, res)
	case "/api/approve/begin":
		return s.approveBegin(body)
	case "/api/approve/finish":
		return s.approveFinish(body, res)
	case "/api/display":
		return map[string]any{"display": intent, "intent_hash": intentHash}, nil
	case "/api/report":
		var in struct{ Step, Name string }
		if json.Unmarshal(body, &in) != nil || !steps[in.Step] {
			return nil, fail(http.StatusBadRequest, "BAD_REQUEST", "")
		}
		// A browser-side outcome (refusal, cancel, SecurityError): recorded as
		// reported by the page, never as a server verification; only the
		// error name, from a fixed list, is kept.
		res.Step = "browser:" + in.Step
		return nil, fail(http.StatusOK, "BROWSER_REPORTED", allowed(in.Name, browserErrors...))
	}
	return nil, fail(http.StatusNotFound, "NOT_FOUND", "")
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

func (s *spike) registerBegin() (any, *apiError) {
	var exclude []protocol.CredentialDescriptor
	for _, c := range s.user.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, session, err := s.wa.BeginRegistration(&s.user,
		webauthn.WithCredentialParameters([]protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}}),
		webauthn.WithExclusions(exclude))
	if err != nil {
		log.Printf("a1-spike: begin: %v", err)
		return nil, fail(http.StatusInternalServerError, "BEGIN_FAILED", "")
	}
	s.reg = session
	return opts, nil
}

func (s *spike) registerFinish(body []byte, res *result) (any, *apiError) {
	if s.reg == nil {
		return nil, fail(http.StatusConflict, "NO_REGISTRATION", "begin first")
	}
	session := *s.reg
	s.reg = nil // one attempt per begin
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "REGISTRATION_MALFORMED", reason(err))
	}
	ad := parsed.Response.AttestationObject.AuthData
	res.Flags, res.Format = flagString(ad.Flags), allowed(parsed.Response.AttestationObject.Format,
		"none", "packed", "tpm", "android-key", "android-safetynet", "fido-u2f", "apple", "compound")
	res.Attachment, res.Credential = allowed(string(parsed.AuthenticatorAttachment), "platform", "cross-platform"), credTag(parsed.RawID)
	for _, t := range parsed.Response.Transports {
		res.Transports = append(res.Transports, allowed(string(t), "usb", "nfc", "ble", "smart-card", "hybrid", "internal"))
	}
	cred, err := s.wa.CreateCredential(&s.user, session, parsed)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "REGISTRATION_INVALID", reason(err))
	}
	if cred.AttestationFormat != "none" { // D6: attestation=none only
		return nil, fail(http.StatusBadRequest, "REGISTRATION_ATTESTATION", "fmt_not_none")
	}
	res.AAGUID = hex.EncodeToString(cred.Authenticator.AAGUID)
	res.SignCount = &cred.Authenticator.SignCount
	s.user.creds = append(s.user.creds, *cred)
	return map[string]any{"credential": res.Credential, "flags": res.Flags, "aaguid": res.AAGUID, "credentials": len(s.user.creds)}, nil
}

func (s *spike) approveBegin(body []byte) (any, *apiError) {
	var in struct{ Session, UV string }
	if json.Unmarshal(body, &in) != nil || (in.Session != "s1" && in.Session != "s2") || (in.UV != "required" && in.UV != "discouraged") {
		return nil, fail(http.StatusBadRequest, "BAD_REQUEST", "session s1|s2, uv required|discouraged")
	}
	var allow []protocol.CredentialDescriptor
	for _, c := range s.user.creds {
		if !s.suspect[string(c.ID)] {
			allow = append(allow, c.Descriptor())
		}
	}
	if len(allow) == 0 {
		return nil, fail(http.StatusConflict, "NO_CREDENTIAL", "register first")
	}
	rnd := make([]byte, 32)
	idb := make([]byte, 12)
	rand.Read(rnd)
	rand.Read(idb)
	session := "spike-session-" + in.Session
	jti := sha256.Sum256([]byte("a1-spike-grant\x00" + session))
	rec := record{Version: 1, ChallengeID: "ch-" + hex.EncodeToString(idb), Random: base64.RawURLEncoding.EncodeToString(rnd),
		SessionID: session, Decision: "approve", ConsumerID: consumerID, WalletID: walletID, IntentHash: intentHash,
		JTI: "grant-" + hex.EncodeToString(jti[:16]), ApproverID: approverID, ApproverVersion: 1, ConfigVersion: configVersion,
		UserVerification: in.UV, ExpiresAt: s.now().Add(challengeTTL).Unix()}
	opts, sd, err := s.wa.BeginLogin(&s.user, webauthn.WithChallenge(rec.challenge()), webauthn.WithAllowedCredentials(allow),
		webauthn.WithUserVerification(protocol.UserVerificationRequirement(in.UV)))
	if err != nil {
		log.Printf("a1-spike: begin: %v", err)
		return nil, fail(http.StatusInternalServerError, "BEGIN_FAILED", "")
	}
	s.ch[rec.ChallengeID] = &pending{rec: rec, session: *sd}
	return map[string]any{"challenge_id": rec.ChallengeID, "options": opts, "record": rec, "display": intent}, nil
}

func (s *spike) approveFinish(body []byte, res *result) (any, *apiError) {
	var in struct {
		ChallengeID string          `json:"challenge_id"`
		Step        string          `json:"step"`
		Credential  json.RawMessage `json:"credential"`
	}
	if json.Unmarshal(body, &in) != nil || !steps[in.Step] || in.Step == "register" {
		return nil, fail(http.StatusBadRequest, "BAD_REQUEST", "")
	}
	res.Step = in.Step
	p := s.ch[in.ChallengeID]
	if p == nil {
		return nil, fail(http.StatusNotFound, "CHALLENGE_UNKNOWN", "")
	}
	res.ChallengeID = p.rec.ChallengeID
	res.Session, res.UVRequested = p.rec.SessionID, p.rec.UserVerification
	parsed, err := protocol.ParseCredentialRequestResponseBytes(in.Credential)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "ASSERTION_MALFORMED", reason(err))
	}
	ad := parsed.Response.AuthenticatorData
	res.Flags, res.Credential, res.Attachment = flagString(ad.Flags), credTag(parsed.RawID), allowed(string(parsed.AuthenticatorAttachment), "platform", "cross-platform")
	res.SignCount = &ad.Counter
	if p.used {
		return nil, fail(http.StatusConflict, "CHALLENGE_USED", "")
	}
	if !s.now().Before(time.Unix(p.rec.ExpiresAt, 0)) {
		return nil, fail(http.StatusConflict, "CHALLENGE_EXPIRED", "")
	}
	i := -1
	for j, c := range s.user.creds {
		if bytes.Equal(c.ID, parsed.RawID) {
			i = j
		}
	}
	if i < 0 {
		return nil, fail(http.StatusBadRequest, "CREDENTIAL_UNKNOWN", "")
	}
	stored := s.user.creds[i]
	res.AAGUID = hex.EncodeToString(stored.Authenticator.AAGUID)
	if s.suspect[string(stored.ID)] {
		return nil, fail(http.StatusConflict, "CREDENTIAL_SUSPECT", "")
	}
	if stored.Flags.BackupEligible != ad.Flags.HasBackupEligible() { // D6: BE is fixed at registration
		s.suspect[string(stored.ID)] = true
		return nil, fail(http.StatusBadRequest, "ASSERTION_INVALID_FLAGS", "backup_eligible_changed")
	}
	// The product policy always requires UV, also when the page asked the
	// browser for "discouraged" (that request only probes the device).
	session := p.session
	session.UserVerification = protocol.VerificationRequired
	cred, err := s.wa.ValidateLogin(&s.user, session, parsed)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "ASSERTION_INVALID", reason(err))
	}
	if cred.Authenticator.CloneWarning { // D6: counter went back (0/0 is accepted by the library)
		s.suspect[string(stored.ID)] = true
		return nil, fail(http.StatusConflict, "ASSERTION_COUNTER", "counter_regressed")
	}
	p.used = true
	s.user.creds[i] = *cred
	return map[string]any{"verified": true, "challenge_id": p.rec.ChallengeID, "approval_session_id": p.rec.SessionID,
		"intent_hash": p.rec.IntentHash, "flags": res.Flags, "sign_count": ad.Counter}, nil
}

// openResults makes a new private run directory under parent and its results
// file (review R2 of 20677b0): parent must be an existing directory of this
// user, not a symlink, with no group or other access; the run directory is
// made with mkdir (fails if anything is there, symlinks included) and the
// file with O_EXCL, so an existing file or link is never appended to and
// nothing outside the new directory is changed.
func openResults(parent, run string) (*os.File, string, error) {
	fi, err := os.Lstat(parent)
	if err != nil {
		return nil, "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.IsDir() || fi.Mode().Perm()&0o077 != 0 || !ok || int(st.Uid) != os.Getuid() {
		return nil, "", fmt.Errorf("%s must be a directory of this user with mode 0700 (not a symlink)", parent)
	}
	dir := filepath.Join(parent, run)
	if err := os.Mkdir(dir, 0o700); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, "results.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

func runName(now time.Time) string {
	b := make([]byte, 4)
	rand.Read(b)
	return "run-" + now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b)
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8123", "loopback address; the page is opened as http://localhost:<port>")
	outDir := flag.String("out-dir", "", "existing private directory (0700); each start makes a new run directory with results.jsonl in it; required")
	label := flag.String("authenticator", "", "real-device or virtual; written on every result; required")
	flag.Parse()
	host, port, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "::1") {
		log.Fatal("a1-spike: --addr must be a loopback address with a port")
	}
	if *outDir == "" || (*label != "real-device" && *label != "virtual") {
		log.Fatal("a1-spike: --out-dir and --authenticator real-device|virtual are required")
	}
	f, out, err := openResults(*outDir, runName(time.Now()))
	if err != nil {
		log.Fatal("a1-spike: results: ", err)
	}
	defer f.Close()
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatal(err)
	}
	_, port, _ = net.SplitHostPort(ln.Addr().String())
	s, err := newSpike(port, *label, f, time.Now)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("a1-spike: test use only, no payment; authenticator=%s; results %s", *label, out)
	log.Printf("a1-spike: listening on http://localhost:%s", port)
	log.Fatal((&http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}).Serve(ln))
}
