package signer

// V1's voice challenge (docs/commercial-v1.md) against a scripted sidecar:
// the three outcomes, replay, the time limit, the service down, a page
// without a microphone, and that nothing of a recording or its text is
// kept. The sidecar's answers are scripted per recording; none of this is
// a model's or a device's result.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
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

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer/signertest"
)

type scripted struct {
	status int
	delay  time.Duration
	an     analysis
}

// fakeSidecar answers /v1/health with its models and /v1/analyze by the
// recording's bytes, as scripted.
type fakeSidecar struct {
	socket string
	mu     sync.Mutex
	script map[string]scripted
	models [2]string
	srv    *http.Server
	calls  int
}

func newFakeSidecar(t *testing.T) *fakeSidecar {
	t.Helper()
	dir, err := os.MkdirTemp("", "v") // a Unix socket path must be short
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	s := &fakeSidecar{socket: filepath.Join(dir, "s"), script: map[string]scripted{}, models: [2]string{"asr@t", "spk@t"}}
	s.start(t)
	return s
}

func (s *fakeSidecar) start(t *testing.T) {
	ln, err := net.Listen("unix", s.socket)
	if err != nil {
		t.Fatal(err)
	}
	s.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		models := s.models
		s.mu.Unlock()
		if r.URL.Path == "/v1/health" {
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "asr": models[0], "speaker": models[1]})
			return
		}
		body, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		sc, ok := s.script[tagOf(body)]
		s.calls++
		s.mu.Unlock()
		if !ok {
			sc = scripted{status: 400, an: analysis{Error: "AUDIO_FORMAT"}}
		}
		time.Sleep(sc.delay)
		if sc.status != 0 {
			w.WriteHeader(sc.status)
		}
		json.NewEncoder(w).Encode(sc.an)
	})}
	go s.srv.Serve(ln)
	t.Cleanup(func() { s.srv.Close() })
}

func (s *fakeSidecar) set(audio string, sc scripted) {
	s.mu.Lock()
	s.script[audio] = sc
	s.mu.Unlock()
}

var (
	speakerA = []float64{1, 0.1, 0, 0}
	speakerB = []float64{0, 0.1, 1, 0}
)

func newVoiceAFX(t *testing.T, s *fakeSidecar) *afx {
	t.Helper()
	f := &afx{t: t, now: 1790726450}
	var c map[string]any
	json.Unmarshal(signertest.ApprovalsConfig("/tmp/apa-test.sock", 501, f.now, addrA, addrB, origin), &c)
	c["voice"] = map[string]any{"socket": s.socket, "timeout_ms": 300, "speaker_threshold_milli": 800, "asr_model": "asr@t", "speaker_model": "spk@t",
		"decimals": 6, "merchants": []map[string]string{{"pay_to": signertest.Merchant, "name": "示意商家", "pinyin": "shi yi shang jia"}}}
	raw, _ := json.Marshal(c)
	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	core, err := NewApprovalCore(context.Background(), cfg, filepath.Join(t.TempDir(), "approvals.db"), func() uint64 { return f.now })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { core.Close() })
	f.core = core
	return f
}

// enrollVoice gives approver a template of speaker A from three recordings.
func enrollVoice(t *testing.T, f *afx, s *fakeSidecar, approver string) {
	t.Helper()
	var wavs [][]byte
	for _, w := range []string{"enroll-1", "enroll-2", "enroll-3"} {
		s.set(w, scripted{an: analysis{Transcript: "x", Embedding: speakerA}})
		wavs = append(wavs, []byte(w))
	}
	if n, err := f.core.EnrollVoice(bgA, approver, wavs); err != nil || n != 3 {
		t.Fatalf("enroll voice: %d %v", n, err)
	}
}

// said is what the sidecar hears when the phrase is read right (as the ASR
// writes it: simplified, a homophone for the merchant), by speaker e.
func said(code string, e []float64) scripted {
	return scripted{an: analysis{Transcript: "核准0.5USDC 付给是一商家，代码" + code, Pinyin: "he zhun 0.5USDC fu gei shi yi shang jia , dai ma " + code, Embedding: e}}
}

func codeOf(t *testing.T, c ApprovalReply) string {
	t.Helper()
	if c.Display == nil || c.Display.Voice == nil {
		t.Fatalf("no voice prompt: %+v", c.Display)
	}
	p := c.Display.Voice.Phrase
	i := strings.LastIndex(p, "代碼 ")
	if !strings.HasPrefix(p, "核准 0.5 USDC，付給示意商家，代碼 ") || i < 0 || c.Display.Voice.MaxSeconds != 10 {
		t.Fatalf("phrase %q", p)
	}
	return strings.ReplaceAll(p[i+len("代碼 "):], " ", "")
}

func (f *afx) verifyVoice(vid, challengeID string, order, assertion json.RawMessage, v *VoiceAnswer) (ApprovalReply, error) {
	return f.core.Verify(bgA, VerifyRequest{VerifyRequestID: vid, ChallengeID: challengeID, Order: order, Assertion: assertion, Voice: v})
}

func audio(s string) *VoiceAnswer {
	return &VoiceAnswer{Audio: base64.StdEncoding.EncodeToString(wavOf(s))}
}

// wavOf is a recording in the page's format whose sound carries tag (the
// scripted sidecar answers by it): at least half a second of 16 kHz mono
// 16-bit PCM.
func wavOf(tag string) []byte {
	n := max(16000, len(tag)+len(tag)%2)
	b := make([]byte, 44+n)
	copy(b, "RIFF")
	binary.LittleEndian.PutUint32(b[4:], uint32(36+n))
	copy(b[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(b[16:], 16)
	binary.LittleEndian.PutUint16(b[20:], 1)
	binary.LittleEndian.PutUint16(b[22:], 1)
	binary.LittleEndian.PutUint32(b[24:], 16000)
	binary.LittleEndian.PutUint32(b[28:], 32000)
	binary.LittleEndian.PutUint16(b[32:], 2)
	binary.LittleEndian.PutUint16(b[34:], 16)
	copy(b[36:], "data")
	binary.LittleEndian.PutUint32(b[40:], uint32(n))
	copy(b[44:], tag)
	return b
}

// tagOf is what a scripted sidecar keys a body by: a WAV's tag, else the body.
func tagOf(body []byte) string {
	if len(body) > 44 && string(body[:4]) == "RIFF" {
		return strings.TrimRight(string(body[44:]), "\x00")
	}
	return string(body)
}

func TestVoicePassResendAndNothingKept(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	a := f.enroll("approver-a", nil)
	enrollVoice(t, f, s, "approver-a")
	order := f.order("a")
	c := f.mustChallenge("req-1", order, "approve")
	code := codeOf(t, c)
	s.set("rec-1", said(code, speakerA))
	as := a.Get(c.Options, origin)
	r, err := f.verifyVoice("ver-1", c.ChallengeID, order, as, audio("rec-1"))
	if err != nil || r.ApprovalID == "" || r.VoiceResult == nil || r.VoiceResult.Result != "verified" || r.VoiceResult.Reason != "" {
		t.Fatalf("verify: %+v %v", r, err)
	}
	calls := s.calls
	again, err := f.verifyVoice("ver-1", c.ChallengeID, order, as, audio("rec-1"))
	if err != nil || again.ApprovalID != r.ApprovalID || s.calls != calls {
		t.Fatalf("a resend is answered from what was saved, without the sidecar: %+v %v", again, err)
	}
	if _, err := f.verifyVoice("ver-1", c.ChallengeID, order, as, audio("rec-other")); refusal(err) != "REQUEST_ID_CONFLICT" {
		t.Fatalf("same verify ID, another recording: %v", err)
	}
	// Neither the recording nor its transcript is in the database.
	f.core.db.Exec(`PRAGMA wal_checkpoint(FULL)`)
	var raw []byte
	for _, n := range []string{"approvals.db", "approvals.db-wal"} {
		b, _ := os.ReadFile(filepath.Join(f.core.dir, n))
		raw = append(raw, b...)
	}
	for _, x := range []string{"rec-1", base64.StdEncoding.EncodeToString([]byte("rec-1")), "核准0.5USDC", "shi yi shang jia , dai ma"} {
		if bytes.Contains(raw, []byte(x)) {
			t.Errorf("the database holds %q", x)
		}
	}
	if fi, err := os.Stat(filepath.Join(f.core.dir, "voice.key")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf("voice.key: %v %v", fi, err)
	}
}

func TestVoiceFailuresDenyTheSession(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	a := f.enroll("approver-a", nil)
	enrollVoice(t, f, s, "approver-a")
	cases := []struct {
		name, audio, want string
		sc                func(code string) scripted
	}{
		{"another speaker", "rec-b", "VOICE_SPEAKER_MISMATCH", func(code string) scripted { return said(code, speakerB) }},
		{"a wrong code", "rec-code", "VOICE_PHRASE_MISMATCH", func(code string) scripted { return said("0000", speakerA) }},
		{"a wrong amount", "rec-amount", "VOICE_PHRASE_MISMATCH", func(code string) scripted {
			sc := said(code, speakerA)
			sc.an.Transcript = strings.Replace(sc.an.Transcript, "0.5", "10.5", 1)
			return sc
		}},
		{"another merchant", "rec-merchant", "VOICE_PHRASE_MISMATCH", func(code string) scripted {
			sc := said(code, speakerA)
			sc.an.Pinyin = strings.Replace(sc.an.Pinyin, "shi yi", "bian li", 1)
			return sc
		}},
		{"an unreadable recording", "rec-bad", "VOICE_AUDIO_INVALID", func(string) scripted { return scripted{status: 400, an: analysis{Error: "AUDIO_FORMAT"}} }},
	}
	for i, tc := range cases {
		order := f.order("a")
		c := f.mustChallenge("req-f"+string(rune('a'+i)), order, "approve")
		s.set(tc.audio, tc.sc(codeOf(t, c)))
		r, err := f.verifyVoice("ver-f"+string(rune('a'+i)), c.ChallengeID, order, a.Get(c.Options, origin), audio(tc.audio))
		if refusal(err) != tc.want || r.VoiceResult == nil || r.VoiceResult.Result != "failed" || r.VoiceResult.Reason != tc.want {
			t.Fatalf("%s: %+v %v", tc.name, r, err)
		}
		if st, _ := f.core.ChallengeByID(bgA, c.ChallengeID); st.State != "closed" {
			t.Errorf("%s: the challenge is %s, want closed", tc.name, st.State)
		}
		if _, err := f.challenge("req-f2"+string(rune('a'+i)), order, "approve"); refusal(err) != "SESSION_DECIDED" {
			t.Errorf("%s: a new challenge for the denied session: %v", tc.name, err)
		}
	}
	// A recording used once is a replay anywhere after (here: a new
	// session, its phrase scripted right); the approval it got stands alone.
	order := f.order("a")
	c := f.mustChallenge("req-r1", order, "approve")
	s.set("rec-once", said(codeOf(t, c), speakerA))
	if _, err := f.verifyVoice("ver-r1", c.ChallengeID, order, a.Get(c.Options, origin), audio("rec-once")); err != nil {
		t.Fatal(err)
	}
	order2 := f.order("a")
	c2 := f.mustChallenge("req-r2", order2, "approve")
	s.set("rec-once", said(codeOf(t, c2), speakerA))
	if r, err := f.verifyVoice("ver-r2", c2.ChallengeID, order2, a.Get(c2.Options, origin), audio("rec-once")); refusal(err) != "VOICE_REPLAY" || r.VoiceResult == nil {
		t.Fatalf("replay: %+v %v", r, err)
	}
}

// The Signer's health names the sidecar's state (gap review C2).
func TestVoiceHealthState(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	ctx := context.Background()
	if got := f.core.voice.voiceState(ctx); got != "ready" {
		t.Fatalf("a healthy sidecar: %s", got)
	}
	s.mu.Lock()
	s.models[0] = "asr@other"
	s.mu.Unlock()
	if got := f.core.voice.voiceState(ctx); got != "unavailable" {
		t.Fatalf("other models: %s", got)
	}
	s.srv.Close()
	os.Remove(s.socket)
	if got := f.core.voice.voiceState(ctx); got != "unavailable" {
		t.Fatalf("down: %s", got)
	}
	if got := (*sidecar)(nil).voiceState(ctx); got != "off" {
		t.Fatalf("no voice configured: %s", got)
	}
}

func TestVoiceUnavailableIsDegradedNotFailed(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	a := f.enroll("approver-a", nil)
	enrollVoice(t, f, s, "approver-a")
	try := func(name string, v *VoiceAnswer, want string) {
		t.Helper()
		order := f.order("a")
		c := f.mustChallenge("req-"+name, order, "approve")
		r, err := f.verifyVoice("ver-"+name, c.ChallengeID, order, a.Get(c.Options, origin), v)
		if err != nil || r.ApprovalID == "" || r.VoiceResult == nil || r.VoiceResult.Result != "unavailable" || r.VoiceResult.Reason != want {
			t.Fatalf("%s: %+v %v", name, r, err)
		}
	}
	try("device", &VoiceAnswer{Unavailable: "device"}, "VOICE_DEVICE_UNAVAILABLE")
	// Late: the scripted answer comes after the time limit; it changes nothing after.
	s.set("rec-late", scripted{delay: 600 * time.Millisecond, an: analysis{Transcript: "x", Embedding: speakerA}})
	try("late", audio("rec-late"), "VOICE_TIMEOUT")
	s.mu.Lock()
	s.models[1] = "spk@other" // a sidecar with other models is not the approved service
	s.mu.Unlock()
	try("models", audio("rec-models"), "VOICE_SERVICE_UNAVAILABLE")
	s.srv.Close()
	os.Remove(s.socket)
	try("down", audio("rec-down"), "VOICE_SERVICE_UNAVAILABLE")
}

func TestVoiceRequestRules(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	a := f.enroll("approver-a", nil)
	order := f.order("a")
	if _, err := f.challenge("req-0", order, "approve"); refusal(err) != "VOICE_NOT_ENROLLED" {
		t.Fatalf("no template: %v", err)
	}
	enrollVoice(t, f, s, "approver-a")
	c := f.mustChallenge("req-1", order, "approve")
	if _, err := f.verifyVoice("ver-1", c.ChallengeID, order, a.Get(c.Options, origin), nil); refusal(err) != "VOICE_REQUIRED" {
		t.Fatalf("no voice: %v", err)
	}
	for _, v := range []*VoiceAnswer{{}, {Audio: "eA==", Unavailable: "device"}, {Unavailable: "mute"}, {Audio: "not base64!"}} {
		if _, err := f.verifyVoice("ver-x", c.ChallengeID, order, a.Get(c.Options, origin), v); refusal(err) != "BAD_REQUEST" {
			t.Errorf("voice %+v: %v", v, err)
		}
	}
	// A deny needs no voice and refuses one.
	order2 := f.order("a")
	d := f.mustChallenge("req-d", order2, "deny")
	if d.Display.Voice != nil {
		t.Fatal("a deny challenge asks for a voice")
	}
	if _, err := f.verifyVoice("ver-d1", d.ChallengeID, order2, a.Get(d.Options, origin), audio("x")); refusal(err) != "BAD_REQUEST" {
		t.Fatalf("voice on a deny: %v", err)
	}
	if r, err := f.verifyVoice("ver-d2", d.ChallengeID, order2, a.Get(d.Options, origin), nil); err != nil || r.Decision != "deny" || r.VoiceResult != nil {
		t.Fatalf("deny: %+v %v", r, err)
	}
	// Deleting the template stops new approve challenges.
	if ok, err := f.core.DeleteVoice(bgA, "approver-a"); !ok || err != nil {
		t.Fatal(ok, err)
	}
	if _, err := f.challenge("req-2", f.order("a"), "approve"); refusal(err) != "VOICE_NOT_ENROLLED" {
		t.Fatalf("after delete: %v", err)
	}
}

func TestPhraseMatching(t *testing.T) {
	for _, c := range []struct {
		units string
		dec   uint
		want  string
	}{{"360000", 6, "0.36"}, {"500000", 6, "0.5"}, {"80000", 6, "0.08"}, {"1000000", 6, "1"}, {"1", 6, "0.000001"}, {"12", 0, "12"}} {
		if got := spokenAmount(contracts.Amount(c.units), c.dec); got != c.want {
			t.Errorf("spokenAmount(%s, %d) = %s, want %s", c.units, c.dec, got, c.want)
		}
	}
	for _, c := range []struct {
		text, n string
		want    bool
	}{
		{"核准0.36TestUSDC", "0.36", true}, {"核准零點三六", "0.36", true}, {"核准 0 点 3 6", "0.36", true}, {"10.36", "0.36", false},
		{"0.365", "0.36", false}, {"代碼4712。", "4712", true}, {"代碼 4、7、1、2", "4712", true}, {"代码四七一二", "4712", true},
		{"代码47123", "4712", false}, {"代碼14712", "4712", false}, {"1.4712", "4712", false},
	} {
		if got := hasNumber(normalize(c.text), c.n); got != c.want {
			t.Errorf("hasNumber(%q, %s) = %v", c.text, c.n, got)
		}
	}
	if a, m, c := matchPhrase("核准0.36USDC 付给相口便利商店,代码4712", "he zhun 0.36USDC fu gei xiang kou bian li shang dian , dai ma 4712", "0.36", "4712", "xiang kou bian li shang dian"); !a || !m || !c {
		t.Errorf("a homophone merchant: %v %v %v", a, m, c)
	}
	// The merchant, loosely: one syllable in four wrong, missing or added.
	for _, c := range []struct {
		pinyin string
		want   bool
	}{
		{"he zhun 0.5USDC , fu gei ce shi shang dian , dai ma 3540", true},
		{"fu gei ce shi , shang dian", true},           // punctuation inside the name
		{"fu gei ce shi , shang chang", true},          // punctuation is not a syllable: one wrong only
		{"fu gei ce shi de shang dian", true},          // one added
		{"fu gei ce shi shang chang", true},            // one wrong
		{"fu gei ce shang dian", true},                 // one missing
		{"fu gei se shi shang chang", false},           // two wrong
		{"fu gei ce shi de de shang dian", false},      // two added
		{"fu gei shi yi shang jia dai ma 3540", false}, // another merchant
		{"", false},
	} {
		if got := merchantNear(c.pinyin, "ce shi shang dian"); got != c.want {
			t.Errorf("merchantNear(%q) = %v", c.pinyin, got)
		}
	}
	if merchantNear("fu gei ce dian", "ce shi") || !merchantNear("ce shi", "ce shi") { // a short name: exact only
		t.Error("a two-syllable merchant is matched exactly")
	}
	if voiceCode("AAAAAA") != "0000" || len(voiceCode("_____w")) != 4 || voiceCode("!") != "" {
		t.Error("voiceCode")
	}
}

// TestVoicePhraseEval is voice/eval.py's use of this package's own phrase
// comparison (so the evaluation judges exactly what the Signer judges):
// skipped unless APA_VOICE_PHRASES names a JSON list of {transcript,
// pinyin, amount, code, merchant_pinyin}; the results go to
// APA_VOICE_PHRASES + ".out" as a list of [amount_ok, merchant_ok, code_ok].
func TestVoicePhraseEval(t *testing.T) {
	in := os.Getenv("APA_VOICE_PHRASES")
	if in == "" {
		t.Skip("evaluation only (voice/eval.py)")
	}
	raw, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Transcript, Pinyin, Amount, Code string
		MerchantPinyin                   string `json:"merchant_pinyin"`
	}
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatal(err)
	}
	var out [][3]bool
	for _, c := range cases {
		a, m, k := matchPhrase(c.Transcript, c.Pinyin, c.Amount, c.Code, c.MerchantPinyin)
		out = append(out, [3]bool{a, m, k})
	}
	res, _ := json.Marshal(out)
	if err := os.WriteFile(in+".out", res, 0o600); err != nil {
		t.Fatal(err)
	}
}

// Review S2-S5: the Signer's own trouble is never an unavailable voice; a
// recording not in the page's format fails before the sidecar; a sidecar
// in a directory others can write gets nothing.
func TestVoiceSignerTroubleAndInputs(t *testing.T) {
	s := newFakeSidecar(t)
	f := newVoiceAFX(t, s)
	a := f.enroll("approver-a", nil)
	enrollVoice(t, f, s, "approver-a")
	verify := func(name string, v *VoiceAnswer) (ApprovalReply, error, ApprovalReply) {
		t.Helper()
		order := f.order("a")
		c := f.mustChallenge("req-"+name, order, "approve")
		if v == nil {
			s.set("rec-"+name, said(codeOf(t, c), speakerA))
			v = audio("rec-" + name)
		}
		r, err := f.verifyVoice("ver-"+name, c.ChallengeID, order, a.Get(c.Options, origin), v)
		st, _ := f.core.ChallengeByID(bgA, c.ChallengeID)
		return r, err, st
	}
	calls := s.calls
	raw := &VoiceAnswer{Audio: base64.StdEncoding.EncodeToString([]byte("not a wav"))}
	if r, err, _ := verify("raw", raw); refusal(err) != "VOICE_AUDIO_INVALID" || s.calls != calls {
		t.Errorf("a non-WAV recording: %+v %v (sidecar calls %d -> %d)", r, err, calls, s.calls)
	}
	s.set("rec-500", scripted{status: 500})
	if r, err, _ := verify("500", audio("rec-500")); err != nil || r.VoiceResult == nil || r.VoiceResult.Reason != "VOICE_SERVICE_UNAVAILABLE" {
		t.Errorf("a sidecar 500: %+v %v", r, err)
	}
	// The socket's directory writable by others: nothing is sent, unavailable.
	dir := filepath.Dir(s.socket)
	os.Chmod(dir, 0o777)
	calls = s.calls
	if r, err, _ := verify("dir", nil); err != nil || r.VoiceResult == nil || r.VoiceResult.Reason != "VOICE_SERVICE_UNAVAILABLE" || s.calls != calls {
		t.Errorf("a socket directory others can write: %+v %v (sidecar calls %d -> %d)", r, err, calls, s.calls)
	}
	os.Chmod(dir, 0o700)
	// The key gone between the challenge and the verify: refused, nothing approved, the challenge still issued.
	order := f.order("a")
	c := f.mustChallenge("req-key", order, "approve")
	s.set("rec-key", said(codeOf(t, c), speakerA))
	key := filepath.Join(f.core.dir, "voice.key")
	saved, _ := os.ReadFile(key)
	os.Remove(key)
	r, err := f.verifyVoice("ver-key", c.ChallengeID, order, a.Get(c.Options, origin), audio("rec-key"))
	if st, _ := f.core.ChallengeByID(bgA, c.ChallengeID); refusal(err) != "VOICE_SIGNER_UNAVAILABLE" || r.ApprovalID != "" || st.State != "issued" {
		t.Errorf("no key at verify: %+v %v, challenge %s", r, err, st.State)
	}
	if _, err := f.challenge("req-nokey", f.order("a"), "approve"); refusal(err) != "VOICE_NOT_ENROLLED" {
		t.Errorf("no key at the challenge: %v", err)
	}
	os.WriteFile(key, saved, 0o644)
	if _, err := f.challenge("req-0644", f.order("a"), "approve"); refusal(err) != "VOICE_NOT_ENROLLED" {
		t.Errorf("a key others can read: %v", err)
	}
	os.Chmod(key, 0o600)
	f.core.db.Exec(`UPDATE voice_templates SET speaker_model = 'spk@old'`)
	if _, err := f.challenge("req-model", f.order("a"), "approve"); refusal(err) != "VOICE_NOT_ENROLLED" {
		t.Errorf("a template of another speaker model: %v", err)
	}
	// An embedding with no length makes no template.
	s.set("zero-1", scripted{an: analysis{Transcript: "x", Embedding: []float64{0, 0, 0, 0}}})
	if _, err := f.core.EnrollVoice(bgA, "approver-a", [][]byte{[]byte("zero-1"), []byte("enroll-2"), []byte("enroll-3")}); err == nil || !strings.Contains(err.Error(), "an unusable embedding") {
		t.Errorf("a zero embedding: %v", err) // refused for what it is, before anything is encoded
	}
	st, err := f.core.VoiceStats(bgA)
	if err != nil || st["failed VOICE_AUDIO_INVALID"] != 1 || st["unavailable VOICE_SERVICE_UNAVAILABLE"] != 2 {
		t.Errorf("stats %v %v", st, err)
	}
}

// Review 6: two merchants whose spoken names pass for each other are refused.
func TestVoiceMerchantsSoundApart(t *testing.T) {
	v := func(pinyins ...string) *Voice {
		w := &Voice{Socket: "/tmp/v.sock", TimeoutMS: 10000, SpeakerThreshold: 365, ASRModel: "a", SpeakerModel: "s", Decimals: 6}
		for i, p := range pinyins {
			w.Merchants = append(w.Merchants, VoiceMerchant{PayTo: "0x" + strings.Repeat(string(rune('3'+i)), 40), Name: "店", Pinyin: p})
		}
		return w
	}
	c := &Config{WebAuthn: &WebAuthn{}}
	if err := v("ce shi shang dian", "xiang kou bian li shang dian").check(c); err != nil {
		t.Fatalf("names apart: %v", err)
	}
	for _, alike := range [][]string{{"ce shi shang dian", "ce shi shang chang"}, {"ce shi shang dian", "ce shi shang"}, {"ce shi", "ce shi"}} {
		if err := v(alike...).check(c); err == nil || !strings.Contains(err.Error(), "too alike") {
			t.Errorf("%v: %v", alike, err)
		}
	}
}
