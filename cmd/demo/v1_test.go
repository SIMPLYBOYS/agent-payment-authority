package main

// V1's voice challenge through the Host (docs/commercial-v1.md): the real
// Signer over its socket with a voice section, a scripted sidecar (its
// answers are set per recording; no model runs) and the software
// authenticator. Pass, fail and unavailable reach the Store as a grant
// with its voice outcome, a denial, or a degraded grant, each after its
// voice event; the database keeps no recording.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type v1Sidecar struct {
	sock   string
	mu     sync.Mutex
	script map[string]map[string]any // recording -> the sidecar's answer
	delay  map[string]time.Duration
}

func newV1Sidecar(t *testing.T) *v1Sidecar {
	t.Helper()
	dir, err := os.MkdirTemp("", "v1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &v1Sidecar{sock: filepath.Join(dir, "v.sock"), script: map[string]map[string]any{}, delay: map[string]time.Duration{}}
	ln, err := net.Listen("unix", s.sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/health" {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "asr": "asr@t", "speaker": "spk@t"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		a, ok := s.script[string(body)]
		d := s.delay[string(body)]
		s.mu.Unlock()
		time.Sleep(d)
		if !ok {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "AUDIO_FORMAT"})
			return
		}
		json.NewEncoder(w).Encode(a)
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return s
}

func (s *v1Sidecar) set(rec string, a map[string]any) { s.mu.Lock(); s.script[rec] = a; s.mu.Unlock() }

var (
	voiceA = []float64{1, 0.2, 0, 0}
	voiceB = []float64{0, 0.2, 1, 0}
)

// heard is the phrase as the ASR writes it (simplified; a homophone of the
// merchant), with code, by speaker e.
func heard(code string, e []float64) map[string]any {
	return map[string]any{"transcript": "核准0.5USDC 付给是一商家，代码" + code, "pinyin": "he zhun 0.5USDC fu gei shi yi shang jia , dai ma " + code, "embedding": e}
}

func startV1(t *testing.T, timeoutMS ...int) (*a2fx, *v1Sidecar) {
	t.Helper()
	s := newV1Sidecar(t)
	tm := 300
	if len(timeoutMS) > 0 {
		tm = timeoutMS[0]
	}
	f := startA2(t, 60, map[string]any{"socket": s.sock, "timeout_ms": tm, "speaker_threshold_milli": 800, "asr_model": "asr@t", "speaker_model": "spk@t",
		"decimals": 6, "merchants": []map[string]string{{"pay_to": "0x3333333333333333333333333333333333333333", "name": "示意商家", "pinyin": "shi yi shang jia"}}})
	var wavs [][]byte
	for _, w := range []string{"enr-1", "enr-2", "enr-3"} {
		s.set(w, map[string]any{"transcript": "x", "pinyin": "x", "embedding": voiceA})
		wavs = append(wavs, []byte(w))
	}
	if _, err := f.core.EnrollVoice(context.Background(), "approver-a", wavs); err != nil {
		t.Fatal(err)
	}
	return f, s
}

// voiceChallenge opens an approve challenge and returns the code its phrase asks for.
func (f *a2fx) voiceChallenge(sid, req string) (map[string]any, string) {
	f.t.Helper()
	code, r := f.challenge(sid, req, "approve")
	if code != 200 {
		f.t.Fatalf("challenge: %d %v", code, r)
	}
	v, _ := r["challenge"].(map[string]any)["display"].(map[string]any)["voice"].(map[string]any)
	p, _ := v["phrase"].(string)
	i := strings.LastIndex(p, "代碼 ")
	if v == nil || i < 0 || !strings.HasPrefix(p, "核准 0.5 USDC，付給示意商家") {
		f.t.Fatalf("voice prompt: %v", r["challenge"])
	}
	return r, strings.ReplaceAll(p[i+len("代碼 "):], " ", "")
}

func (f *a2fx) assertVoice(session, req, verify string, as json.RawMessage, voice map[string]string) (int, map[string]any) {
	return f.post("/api/approvals/"+session+"/assertion", f.h.tokens["viewer:consumer-a"],
		map[string]any{"request_id": req, "verify_request_id": verify, "assertion": as, "voice": voice})
}

func rec(s string) map[string]string {
	return map[string]string{"audio": base64.StdEncoding.EncodeToString([]byte(s))}
}

func types2(evs []eventView) string {
	var out []string
	for _, e := range evs {
		out = append(out, e.EventType)
	}
	return strings.Join(out, ",")
}

func TestV1VoicePass(t *testing.T) {
	f, s := startV1(t)
	sid, intent, task := f.ask("01")
	r, code := f.voiceChallenge(sid, "att-req-0000000000001")
	s.set("rec-pass", heard(code, voiceA))
	st, out := f.assertVoice(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "approve"), rec("rec-pass"))
	if st != 200 || out["outcome"] != "approved" || sessionState(out) != "approved" {
		t.Fatalf("assertion: %d %v", st, out)
	}
	ev := f.h.events(task).Events
	if got := types2(ev[len(ev)-3:]); got != "VOICE_CHALLENGE_VERIFIED,APPROVED,GRANT_ISSUED" {
		t.Fatalf("events %s", got)
	}
	g := f.h.task(task).Purchases[0].Approval.Grant
	if g == nil || g.Voice != "verified" || g.VoiceReason != "" {
		t.Fatalf("grant %+v", g)
	}
	// No recording or transcript in the Host's database or log.
	for _, name := range []string{"demo.db", "demo.db-wal"} {
		raw, _ := os.ReadFile(filepath.Join(f.h.dir, name))
		for _, x := range []string{"rec-pass", base64.StdEncoding.EncodeToString([]byte("rec-pass")), "核准0.5USDC"} {
			if bytes.Contains(raw, []byte(x)) {
				t.Fatalf("%s holds %q", name, x)
			}
		}
	}
	// After a restart the record (voice event included) passes recovery.
	f.h.stop()
	f.h = controlHost(t, hostOpts{dir: f.h.dir, expiryEvery: time.Hour, scenarios: payCatalog(t, 60), args: []string{"--approvals-signer", f.sock}})
	if g := f.h.task(task).Purchases[0].Approval.Grant; g == nil || g.Voice != "verified" {
		t.Fatalf("after restart %+v", g)
	}
}

func TestV1VoiceFailDenies(t *testing.T) {
	f, s := startV1(t)
	sid, intent, task := f.ask("01")
	r, code := f.voiceChallenge(sid, "att-req-0000000000001")
	s.set("rec-other", heard(code, voiceB))
	st, out := f.assertVoice(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "approve"), rec("rec-other"))
	if st != 200 || out["outcome"] != "denied" || out["code"] != "VOICE_SPEAKER_MISMATCH" || attempt(out)["state"] != "closed" || attempt(out)["error_code"] != "VOICE_SPEAKER_MISMATCH" {
		t.Fatalf("assertion: %d %v %v; log %s", st, out["error"], out["code"], tail(f.h.out.String(), 300))
	}
	ev := f.h.events(task).Events
	last := ev[len(ev)-2:]
	if last[0].EventType != "VOICE_CHALLENGE_FAILED" || last[0].Code != "VOICE_SPEAKER_MISMATCH" || last[1].EventType != "APPROVAL_DENIED" ||
		last[1].Decision.Reason != "VOICE_CHALLENGE_FAILED" || last[1].AuthorizationMethod != "" {
		t.Fatalf("events %+v", last)
	}
	if f.h.grants() != 0 {
		t.Fatal("a failed voice challenge issued a grant")
	}
	// The session is over: a new attempt cannot reach the Signer.
	if code, again := f.challenge(sid, "att-req-0000000000002", "approve"); code == 200 {
		t.Fatalf("a new challenge after the denial: %d %v", code, again)
	}
	f.h.stop()
	f.h = controlHost(t, hostOpts{dir: f.h.dir, expiryEvery: time.Hour, scenarios: payCatalog(t, 60), args: []string{"--approvals-signer", f.sock}})
	if v := f.h.task(task); v.Purchases[0].Approval.State != "denied" {
		t.Fatalf("after restart %+v", v.Purchases[0].Approval)
	}
}

func TestV1VoiceUnavailableDegrades(t *testing.T) {
	for _, c := range []struct {
		name, want string
		voice      func(s *v1Sidecar, code string) map[string]string
	}{
		{"no microphone", "VOICE_DEVICE_UNAVAILABLE", func(*v1Sidecar, string) map[string]string { return map[string]string{"unavailable": "device"} }},
		{"late", "VOICE_TIMEOUT", func(s *v1Sidecar, code string) map[string]string {
			s.set("rec-late", heard(code, voiceA))
			s.mu.Lock()
			s.delay["rec-late"] = 600 * time.Millisecond
			s.mu.Unlock()
			return rec("rec-late")
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, s := startV1(t)
			sid, intent, task := f.ask("01")
			r, code := f.voiceChallenge(sid, "att-req-0000000000001")
			st, out := f.assertVoice(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "approve"), c.voice(s, code))
			if st != 200 || out["outcome"] != "approved" {
				t.Fatalf("assertion: %d %v", st, out)
			}
			ev := f.h.events(task).Events
			v := ev[len(ev)-3]
			if v.EventType != "VOICE_UNAVAILABLE" || v.Code != c.want {
				t.Fatalf("events %s %+v", types2(ev), v)
			}
			if g := f.h.task(task).Purchases[0].Approval.Grant; g == nil || g.Voice != "unavailable" || g.VoiceReason != c.want {
				t.Fatalf("grant %+v", g)
			}
		})
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// A recording of a real size (about ten seconds of WAV) and a sidecar that
// takes longer than the Signer's usual 5 s, within its voice time limit:
// neither is cut by a size or time limit on the way (Host, Signer).
func TestV1VoiceRealSizeAndSlow(t *testing.T) {
	f, s := startV1(t, 7000)
	sid, intent, _ := f.ask("01")
	r, code := f.voiceChallenge(sid, "att-req-0000000000001")
	big := strings.Repeat("w", 320000)
	s.set(big, heard(code, voiceA))
	s.mu.Lock()
	s.delay[big] = 6 * time.Second
	s.mu.Unlock()
	st, out := f.assertVoice(sid, "att-req-0000000000001", "ver-req-0000000000001", f.sign(r, sid, intent, "approve"), rec(big))
	if st != 200 || out["outcome"] != "approved" {
		t.Fatalf("assertion: %d %v %v", st, out["error"], out["code"])
	}
	if g := out["item"].(map[string]any)["purchase"].(map[string]any)["approval"].(map[string]any)["grant"].(map[string]any); g["voice"] != "verified" {
		t.Fatalf("grant %v", g)
	}
}
