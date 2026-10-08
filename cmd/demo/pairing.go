package main

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Host pairing (docs/cli-poc.md 56): with --pairing, the host makes one
// short code at start and shows it on its own terminal only (never the log,
// a URL or a file). The /host page trades it once for this run's host,
// operator and viewer tokens: the same tokens and roles as the token file,
// which stays as the advanced way. It is a way to hand over the existing
// credentials, not a control API without them.

const (
	pairingTTL         = 5 * time.Minute
	pairingMaxFailures = 5
	pairingAlphabet    = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567" // Base32: 32 divides 256, so byte%32 is uniform
	pairingLen         = 12
)

// pairing is one run's pairing window: open until the code is used, it
// expires (monotonic time) or it has failed pairingMaxFailures times;
// nothing reopens it or makes a new code.
type pairing struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	failures int
	closed   bool
	now      func() time.Time
}

// newPairing makes a code from crypto/rand: 12 Base32 characters, shown
// in three groups of four.
func newPairing(now func() time.Time) (*pairing, string, error) {
	b := make([]byte, pairingLen)
	if _, err := rand.Read(b); err != nil {
		return nil, "", err
	}
	for i := range b {
		b[i] = pairingAlphabet[int(b[i])%len(pairingAlphabet)]
	}
	code := string(b)
	return &pairing{code: code, expires: now().Add(pairingTTL), now: now}, code[0:4] + "-" + code[4:8] + "-" + code[8:12], nil
}

// normalizeCode takes a typed code (any case, dashes or spaces between
// groups); "" when it is not 12 Base32 characters.
func normalizeCode(s string) string {
	s = strings.ToUpper(strings.NewReplacer("-", "", " ", "").Replace(s))
	if len(s) != pairingLen || strings.Trim(s, pairingAlphabet) != "" {
		return ""
	}
	return s
}

// redeem uses the code once. result: ok | wrong | closed (used, expired or
// too many failures; also for every request after that).
func (p *pairing) redeem(typed string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return "closed"
	}
	if !p.now().Before(p.expires) {
		p.closed = true
		return "closed"
	}
	if c := normalizeCode(typed); c != "" && subtle.ConstantTimeCompare([]byte(c), []byte(p.code)) == 1 {
		p.closed = true // used: one success only
		return "ok"
	}
	p.failures++
	if p.failures >= pairingMaxFailures {
		p.closed = true
	}
	return "wrong"
}

// pair is POST /api/host/pair: a same-origin JSON POST from this host's
// own page (Host and Origin are this address; no Origin, "null" or any
// other origin is refused), at most 1 KiB, exactly {"code": "..."}. Only a
// right code in an open window answers with the tokens; every answer is
// no-store (reply), and nothing of the code is logged.
func (a *api) pair(w http.ResponseWriter, r *http.Request) {
	if r.Host != a.addr {
		fail(w, http.StatusForbidden, "forbidden_host")
		return
	}
	if r.Header.Get("Origin") != "http://"+a.addr {
		fail(w, http.StatusForbidden, "forbidden_origin")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !strictJSON(w, r, &req) {
		return
	}
	switch a.pairing.redeem(req.Code) {
	case "ok":
		fmt.Fprintln(a.out, "demo: pairing: the host page paired; the code is used")
		reply(w, http.StatusOK, map[string]any{
			"note":   "Demo capabilities for this run of the demo host only, handed over once by pairing. Not a user login.",
			"tokens": a.tokens,
		})
	case "closed":
		fail(w, http.StatusGone, "pairing_closed")
	default:
		fail(w, http.StatusForbidden, "pairing_failed")
	}
}

// showPairingCode writes the code on the operator's terminal, once.
func showPairingCode(out io.Writer, code, addr string) error {
	_, err := fmt.Fprintf(out, "\n  主持人配對碼（%d 分鐘內有效、只能用一次、只顯示這一次）：%s\n  請在 http://%s/host 的「配對」欄輸入。\n\n", int(pairingTTL.Minutes()), code, addr)
	return err
}
