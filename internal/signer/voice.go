package signer

// V1 voice challenge (docs/commercial-v1.md): an approve challenge carries a
// voice nonce, and the phrase the approver reads names the amount, the
// merchant and a code from the nonce. At verify, after the assertion
// passed, the Signer sends the recording to the local sidecar (scores
// only), then judges it here against thresholds fixed in its
// configuration:
//
//	verified     the phrase's amount, merchant and code, and the speaker's template
//	failed       a replayed recording, a wrong phrase, another speaker, an unreadable recording:
//	             no approval, the challenge closed, and the session can never be approved
//	unavailable  the sidecar unreachable, unhealthy or late (D4), or the page has no microphone:
//	             the approval stands, recorded as degraded
//
// The recording and its transcript stay in memory; only the result, the
// scores, the thresholds, the models and the recording's SHA-256 are kept.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
)

// Voice is the voice challenge's configuration. Without it no challenge
// asks for one and approvals are as in A2.
type Voice struct {
	Socket           string          `json:"socket"`                  // absolute path of the sidecar's Unix socket
	TimeoutMS        uint64          `json:"timeout_ms"`              // D4: past it, unavailable (1 to 10000)
	SpeakerThreshold uint64          `json:"speaker_threshold_milli"` // D5: cosine x 1000 (canonical JSON has no fractions), from the evaluation's tuning split
	ASRModel         string          `json:"asr_model"`               // the versions the sidecar must report
	SpeakerModel     string          `json:"speaker_model"`
	Decimals         uint            `json:"decimals"`  // the asset's, to speak the amount
	Merchants        []VoiceMerchant `json:"merchants"` // the spoken name of each payee
}

// VoiceMerchant is a payee's spoken name and its toneless pinyin (what a
// transcript is compared with: an ASR homophone is the same merchant).
type VoiceMerchant struct {
	PayTo  string `json:"pay_to"`
	Name   string `json:"name"`
	Pinyin string `json:"pinyin"`
}

var pinyinPattern = regexp.MustCompile(`^[a-z]+( [a-z]+)*$`)

func (v *Voice) check(c *Config) error {
	switch {
	case c.WebAuthn == nil:
		return errors.New("config: voice needs the webauthn section")
	case !filepath.IsAbs(v.Socket):
		return errors.New("config: voice socket must be an absolute path")
	case v.TimeoutMS == 0 || v.TimeoutMS > 10000:
		return errors.New("config: voice timeout 1 to 10000 ms")
	case v.SpeakerThreshold == 0 || v.SpeakerThreshold >= 1000:
		return errors.New("config: voice speaker threshold 1 to 999 (thousandths)")
	case v.ASRModel == "" || v.SpeakerModel == "" || v.Decimals > 18 || len(v.Merchants) == 0:
		return errors.New("config: voice models, decimals and merchants")
	}
	for i, m := range v.Merchants {
		if !addressPattern.MatchString(m.PayTo) || m.Name == "" || len(m.Name) > 60 || !pinyinPattern.MatchString(m.Pinyin) {
			return fmt.Errorf("config: voice merchant %q", m.PayTo)
		}
		// The spoken name is matched loosely (merchantNear): two merchants
		// whose names pass for each other would make it confirm nothing.
		for _, o := range v.Merchants[:i] {
			if merchantNear(m.Pinyin, o.Pinyin) || merchantNear(o.Pinyin, m.Pinyin) {
				return fmt.Errorf("config: voice merchants %q and %q sound too alike", o.PayTo, m.PayTo)
			}
		}
	}
	return nil
}

func (v *Voice) threshold() float64 { return float64(v.SpeakerThreshold) / 1000 }

// requestTimeout bounds one request: the usual time, and with a voice
// section the sidecar's time limit on top (a verify waits for it).
func (c *Config) requestTimeout() time.Duration {
	if c.Voice == nil {
		return timeout
	}
	return timeout + time.Duration(c.Voice.TimeoutMS)*time.Millisecond
}

func (v *Voice) merchant(payTo string) *VoiceMerchant {
	for i := range v.Merchants {
		if v.Merchants[i].PayTo == payTo {
			return &v.Merchants[i]
		}
	}
	return nil
}

const voiceSchema = `
CREATE TABLE IF NOT EXISTS voice_templates (
  approver_id   TEXT PRIMARY KEY,
  sealed        TEXT NOT NULL,
  samples       INTEGER NOT NULL,
  speaker_model TEXT NOT NULL,
  created_at    INTEGER NOT NULL
) STRICT;
CREATE TABLE IF NOT EXISTS voice_results (
  challenge_id      TEXT PRIMARY KEY REFERENCES approval_challenges,
  verify_request_id TEXT NOT NULL UNIQUE,
  jti               TEXT NOT NULL,
  result            TEXT NOT NULL CHECK (result IN ('verified', 'failed', 'unavailable')),
  reason            TEXT,
  amount_ok         INTEGER,
  merchant_ok       INTEGER,
  code_ok           INTEGER,
  speaker_score     REAL,
  speaker_threshold REAL,
  audio_sha256      TEXT UNIQUE,
  asr_model         TEXT,
  speaker_model     TEXT,
  created_at        INTEGER NOT NULL,
  CHECK ((result = 'verified') = (reason IS NULL))
) STRICT;
CREATE INDEX IF NOT EXISTS voice_results_jti ON voice_results (jti, result);
`

// VoicePrompt is what the page shows to be read (in the challenge's display).
type VoicePrompt struct {
	Phrase     string `json:"phrase"`
	MaxSeconds int    `json:"max_seconds"`
}

// VoiceAnswer is the page's half of a verify request: the recording (a
// 16 kHz mono 16-bit WAV, base64), or that it has no microphone (D4 a).
type VoiceAnswer struct {
	Audio       string `json:"audio,omitempty"`
	Unavailable string `json:"unavailable,omitempty"` // "device"
}

// VoiceResult is the judged outcome, as an approval and a refusal report it.
type VoiceResult struct {
	Result string `json:"result"`           // verified | failed | unavailable
	Reason string `json:"reason,omitempty"` // a fixed code, unless verified
}

const (
	voiceMaxSeconds = 10
	voiceMaxWAV     = 44 + voiceMaxSeconds*16000*2
	voiceMinSamples = 3
)

// voiceCode is the four digits the nonce asks to be read.
func voiceCode(nonce string) string {
	b, err := base64.RawURLEncoding.DecodeString(nonce)
	if err != nil || len(b) < 4 {
		return ""
	}
	return fmt.Sprintf("%04d", binary.BigEndian.Uint32(b[:4])%10000)
}

// spokenAmount is base units as a decimal without trailing zeros.
func spokenAmount(a contracts.Amount, decimals uint) string {
	s := strings.TrimLeft(string(a), "0")
	if decimals == 0 {
		return cmpOr(s, "0")
	}
	if len(s) <= int(decimals) {
		s = strings.Repeat("0", int(decimals)-len(s)+1) + s
	}
	whole, frac := s[:len(s)-int(decimals)], strings.TrimRight(s[len(s)-int(decimals):], "0")
	if frac == "" {
		return whole
	}
	return whole + "." + frac
}

func cmpOr(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// voicePhrase is the challenge's phrase, from the order's amount and payee
// as the Signer read them: "核准 0.36 USDC，付給示意商家，代碼 4 7 1 2".
func (v *Voice) phrase(d Display, nonce string) (string, error) {
	m := v.merchant(d.PayTo)
	code := voiceCode(nonce)
	if m == nil || code == "" {
		return "", Refusal("VOICE_MERCHANT_UNKNOWN")
	}
	asset := cmpOr(d.AssetName, "token")
	return "核准 " + spokenAmount(d.Amount, v.Decimals) + " " + asset + "，付給" + m.Name + "，代碼 " + strings.Join(strings.Split(code, ""), " "), nil
}

var numerals = strings.NewReplacer("零", "0", "〇", "0", "一", "1", "壹", "1", "二", "2", "兩", "2", "两", "2", "三", "3", "四", "4", "五", "5",
	"六", "6", "七", "7", "八", "8", "九", "9", "點", ".", "点", ".", "０", "0", "１", "1", "２", "2", "３", "3", "４", "4", "５", "5", "６", "6",
	"７", "7", "８", "8", "９", "9", "．", ".")

// normalize keeps digits and points of a transcript, numerals as digits,
// with what separates two digits (spaces, commas) dropped.
func normalize(t string) string {
	t = numerals.Replace(t)
	var b strings.Builder
	rs := []rune(t)
	for i, r := range rs {
		isSep := r == ' ' || r == ',' || r == '，' || r == '、' || r == '-'
		num := func(r rune) bool { return isDigit(r) || r == '.' }
		if isSep && i > 0 && i+1 < len(rs) && num(rs[i-1]) && num(rs[i+1]) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// hasNumber: s holds n as a whole number: no digit right before or after
// it, and no point joining it to more digits.
func hasNumber(s, n string) bool {
	digit := func(i int) bool { return i >= 0 && i < len(s) && s[i] >= '0' && s[i] <= '9' }
	for i := 0; i+len(n) <= len(s); i++ {
		if s[i:i+len(n)] != n {
			continue
		}
		j := i + len(n)
		if !digit(i-1) && !(i > 0 && s[i-1] == '.' && digit(i-2)) && !digit(j) && !(j < len(s) && s[j] == '.' && digit(j+1)) {
			return true
		}
	}
	return false
}

// matchPhrase is D5's structured comparison: the amount and the code as
// numbers, exactly; the merchant by its pinyin, loosely (merchantNear).
func matchPhrase(transcript, pinyin, amount, code, merchantPinyin string) (amountOK, merchantOK, codeOK bool) {
	n := normalize(transcript)
	return hasNumber(n, amount), merchantNear(pinyin, merchantPinyin), hasNumber(n, code)
}

// merchantNear: the merchant's toneless syllables appear in the
// transcript's, with at most one syllable wrong, missing or added per four
// (Aaron 2026-10-16, after his device round: 「我覺得可以放寬比對規則」; the
// merchant only: the amount and the code, the replay guard, stay exact). A
// syllable is a run of letters: punctuation, digits and Latin runs such as
// "0.5USDC" are not syllables and never split the name. The binding to the
// payee is the intent hash; the spoken name is the person's confirmation.
func merchantNear(pinyin, merchant string) bool {
	syl := func(s string) []string {
		var out []string
		for _, w := range strings.Fields(strings.ToLower(s)) {
			if w != "" && strings.Trim(w, "abcdefghijklmnopqrstuvwxyzü") == "" {
				out = append(out, w)
			}
		}
		return out
	}
	hay, name := syl(pinyin), syl(merchant)
	if len(name) == 0 {
		return false
	}
	// Approximate substring matching (Sellers): row[i] is the fewest edits
	// that turn name[:i] into a stretch of hay ending here.
	row := make([]int, len(name)+1)
	for i := range row {
		row[i] = i
	}
	best := row[len(name)]
	for _, h := range hay {
		prev := row[0] // the stretch may start anywhere: row[0] stays 0
		for i := 1; i <= len(name); i++ {
			cur := row[i]
			cost := 1
			if name[i-1] == h {
				cost = 0
			}
			row[i] = min(prev+cost, row[i]+1, row[i-1]+1)
			prev = cur
		}
		best = min(best, row[len(name)])
	}
	return best <= len(name)/4
}

func cosine(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return -1
	}
	var dot, na, nb float64
	for i := range a {
		dot, na, nb = dot+a[i]*b[i], na+a[i]*a[i], nb+b[i]*b[i]
	}
	if na == 0 || nb == 0 {
		return -1
	}
	return dot / math.Sqrt(na*nb)
}

// sidecar talks to the voice sidecar over its Unix socket.
type sidecar struct {
	cfg    *Voice
	client *http.Client
}

func newSidecar(v *Voice) *sidecar {
	return &sidecar{cfg: v, client: &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true, // every request dials, so every request is checked
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			// Review S4: whoever is behind the socket gets the recording and
			// answers for the models. Only a directory of the Signer's own
			// user that nobody else can write, and a process of that user.
			if err := ownDir(filepath.Dir(v.Socket)); err != nil {
				return nil, err
			}
			var d net.Dialer
			c, err := d.DialContext(ctx, "unix", v.Socket)
			if err != nil {
				return nil, err
			}
			uc, ok := c.(*net.UnixConn)
			if uid, err := peerUID(uc); !ok || err != nil || uid != uint32(os.Getuid()) {
				c.Close()
				return nil, errors.New("voice sidecar: not the Signer's user")
			}
			return c, nil
		}}}}
}

// ownDir: dir is the Signer's user's, and no one else can write in it.
func ownDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) || fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("voice sidecar: %s must be the Signer's user's and writable by no one else", dir)
	}
	return nil
}

// wavOK: the page's format, a 16 kHz mono 16-bit PCM WAV (44-byte header)
// of half a second to the limit. Anything else is a failed recording,
// decided here, never the sidecar's (review S5).
func wavOK(b []byte) bool {
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" || string(b[12:16]) != "fmt " || string(b[36:40]) != "data" {
		return false
	}
	le := binary.LittleEndian
	n := le.Uint32(b[40:44])
	return le.Uint32(b[16:20]) == 16 && le.Uint16(b[20:22]) == 1 && le.Uint16(b[22:24]) == 1 && le.Uint32(b[24:28]) == 16000 &&
		le.Uint16(b[34:36]) == 16 && int(n) == len(b)-44 && n >= 16000 && n <= 16000*2*voiceMaxSeconds
}

type analysis struct {
	Transcript string    `json:"transcript"`
	Pinyin     string    `json:"pinyin"`
	Embedding  []float64 `json:"embedding"`
	Error      string    `json:"error"`
}

// errUnavailable is the sidecar not answering as it should (D4).
var errUnavailable = errors.New("voice sidecar unavailable")

func (s *sidecar) get(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, "http://voice"+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "audio/wav")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, raw, err
}

// healthy is the sidecar answering its health with the configured models.
func (s *sidecar) healthy(ctx context.Context) error {
	code, raw, err := s.get(ctx, http.MethodGet, "/v1/health", nil)
	if err != nil {
		return errors.Join(errUnavailable, err)
	}
	var h struct {
		OK      bool   `json:"ok"`
		ASR     string `json:"asr"`
		Speaker string `json:"speaker"`
	}
	if code != http.StatusOK || json.Unmarshal(raw, &h) != nil || !h.OK || h.ASR != s.cfg.ASRModel || h.Speaker != s.cfg.SpeakerModel {
		return errUnavailable
	}
	return nil
}

// voiceState is the Signer's health line for the sidecar (gap review C2):
// off without a voice configuration, else ready or unavailable.
func (s *sidecar) voiceState(ctx context.Context) string {
	if s == nil {
		return "off"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if s.healthy(ctx) != nil {
		return "unavailable"
	}
	return "ready"
}

// analyze checks the sidecar's health and models, then has it score wav.
// A 400 is the recording's fault (failed); anything else that goes wrong
// is the service's (unavailable), and so is the context's deadline.
func (s *sidecar) analyze(ctx context.Context, wav []byte) (analysis, error) {
	var a analysis
	if err := s.healthy(ctx); err != nil {
		return a, err
	}
	code, raw, err := s.get(ctx, http.MethodPost, "/v1/analyze", wav)
	if err != nil {
		return a, errors.Join(errUnavailable, err)
	}
	if json.Unmarshal(raw, &a) != nil {
		return a, errUnavailable
	}
	switch {
	case code == http.StatusBadRequest:
		return a, Refusal("VOICE_AUDIO_INVALID")
	case code != http.StatusOK || len(a.Embedding) == 0:
		return a, errUnavailable
	}
	return a, nil
}

// voiceOutcome is the judged result with what is kept of it.
type voiceOutcome struct {
	VoiceResult
	amountOK, merchantOK, codeOK *bool
	score                        *float64
	audioSHA                     string
}

// judgeVoice scores the page's answer for challenge a (whose record holds
// the nonce), within the configured time limit.
// An error is the Signer's own trouble (its database, its key, its
// template or configuration): the verify is refused, nothing is approved,
// and it is never read as an unavailable voice (review S3).
func (c *ApprovalCore) judgeVoice(ctx context.Context, a challengeRow, ans *VoiceAnswer) (voiceOutcome, error) {
	v := c.cfg.Voice
	if ans.Unavailable == "device" {
		return voiceOutcome{VoiceResult: VoiceResult{"unavailable", "VOICE_DEVICE_UNAVAILABLE"}}, nil
	}
	var d Display
	_ = json.Unmarshal([]byte(a.display), &d)
	m := v.merchant(d.PayTo)
	tpl, err := c.voiceTemplate(ctx, a.approver)
	if m == nil || err != nil {
		return voiceOutcome{}, errors.Join(errors.New("voice: merchant or template"), err)
	}
	wav, _ := base64.StdEncoding.DecodeString(ans.Audio) // checked by Verify
	sum := sha256.Sum256(wav)
	out := voiceOutcome{audioSHA: hex.EncodeToString(sum[:])}
	var seen int
	if err := c.db.QueryRowContext(ctx, `SELECT count(*) FROM voice_results WHERE audio_sha256 = ?`, out.audioSHA).Scan(&seen); err != nil {
		return voiceOutcome{}, err
	} else if seen > 0 {
		return voiceOutcome{VoiceResult: VoiceResult{"failed", "VOICE_REPLAY"}}, nil // the hash stays the first use's
	}
	if !wavOK(wav) {
		out.VoiceResult = VoiceResult{"failed", "VOICE_AUDIO_INVALID"}
		return out, nil
	}
	tctx, cancel := context.WithTimeout(ctx, time.Duration(v.TimeoutMS)*time.Millisecond)
	defer cancel() // a result past the deadline is never read: the request is gone with the context
	an, err := c.voice.analyze(tctx, wav)
	switch {
	case errors.Is(err, Refusal("VOICE_AUDIO_INVALID")):
		out.VoiceResult = VoiceResult{"failed", "VOICE_AUDIO_INVALID"}
		return out, nil
	case err != nil && tctx.Err() != nil: // review S2: an analysis that did come back is always judged
		out.VoiceResult = VoiceResult{"unavailable", "VOICE_TIMEOUT"}
		return out, nil
	case err != nil:
		out.VoiceResult = VoiceResult{"unavailable", "VOICE_SERVICE_UNAVAILABLE"}
		return out, nil
	}
	am, mm, cm := matchPhrase(an.Transcript, an.Pinyin, spokenAmount(d.Amount, v.Decimals), voiceCode(a.voiceNonce), m.Pinyin)
	score := cosine(an.Embedding, tpl)
	out.amountOK, out.merchantOK, out.codeOK, out.score = &am, &mm, &cm, &score
	switch {
	case !am || !mm || !cm:
		out.VoiceResult = VoiceResult{"failed", "VOICE_PHRASE_MISMATCH"}
	case !(score >= v.threshold()): // a NaN score is no match
		out.VoiceResult = VoiceResult{"failed", "VOICE_SPEAKER_MISMATCH"}
	default:
		out.VoiceResult = VoiceResult{Result: "verified"}
	}
	return out, nil
}

// saveVoice keeps an outcome in tx (never the recording or its text).
func (c *ApprovalCore) saveVoice(ctx context.Context, tx *sql.Tx, now uint64, a challengeRow, verifyRequestID string, o voiceOutcome) error {
	v := c.cfg.Voice
	var reason, sha any
	if o.Reason != "" {
		reason = o.Reason
	}
	if o.audioSHA != "" {
		sha = o.audioSHA
	}
	b := func(p *bool) any {
		if p == nil {
			return nil
		}
		if *p {
			return 1
		}
		return 0
	}
	var score, threshold any
	if o.score != nil {
		score, threshold = *o.score, v.threshold()
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO voice_results (challenge_id, verify_request_id, jti, result, reason, amount_ok, merchant_ok, code_ok,
		speaker_score, speaker_threshold, audio_sha256, asr_model, speaker_model, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.id, verifyRequestID, a.jti, o.Result, reason, b(o.amountOK), b(o.merchantOK), b(o.codeOK), score, threshold, sha, v.ASRModel, v.SpeakerModel, now)
	return err
}

// voiceFail closes challenge a with a failed voice outcome, in one write
// transaction, and saves the refusal: the session is denied (the Host
// records it) and the Signer never issues it another challenge.
func (c *ApprovalCore) voiceFail(ctx context.Context, r VerifyRequest, fp string, a challengeRow, o voiceOutcome) (ApprovalReply, error) {
	code := o.Reason
	err := c.inTx(ctx, func(tx *sql.Tx, now uint64) error {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM verify_requests WHERE verify_request_id = ?`, r.VerifyRequestID).Scan(&n); err != nil {
			return err
		} else if n > 0 {
			return Refusal("REQUEST_ID_RACE")
		}
		var state string
		var expiresAt uint64
		if err := tx.QueryRowContext(ctx, `SELECT state, expires_at FROM approval_challenges WHERE challenge_id = ?`, a.id).Scan(&state, &expiresAt); err != nil {
			return err
		}
		if state != "issued" || now >= expiresAt { // decided by its state first, as commit does
			code = map[string]string{"closed": "CHALLENGE_CLOSED", "used": "CHALLENGE_USED"}[state]
			if code == "" {
				code = "CHALLENGE_EXPIRED"
				if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'expired', closed_at = ? WHERE challenge_id = ? AND state = 'issued'`, now, a.id); err != nil {
					return err
				}
			}
			return c.saveRefusal(ctx, tx, now, r, fp, a.id, code, false)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE approval_challenges SET state = 'closed', closed_at = ? WHERE challenge_id = ? AND state = 'issued'`, now, a.id); err != nil {
			return err
		}
		if err := c.saveVoice(ctx, tx, now, a, r.VerifyRequestID, o); err != nil {
			return err
		}
		return c.saveRefusal(ctx, tx, now, r, fp, a.id, code, false)
	})
	if errors.Is(err, Refusal("REQUEST_ID_RACE")) {
		rep, _, err := c.verifyAnswer(ctx, r.VerifyRequestID, fp)
		return rep, err
	}
	if err != nil {
		return ApprovalReply{}, err
	}
	rep := ApprovalReply{ChallengeID: a.id, VerifyRequestID: r.VerifyRequestID, Error: code}
	if code == o.Reason {
		rep.VoiceResult = &o.VoiceResult
	}
	return rep, Refusal(code)
}

// voiceFailed: the session has a failed voice result (it can never be approved).
func (c *ApprovalCore) voiceFailed(ctx context.Context, q querier, jti string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT count(*) FROM voice_results WHERE jti = ? AND result = 'failed'`, jti).Scan(&n)
	return n > 0, err
}

// ---- the speaker template: sealed with a key in the Signer's directory ----

func (c *ApprovalCore) voiceKey(create bool) ([]byte, error) {
	path := filepath.Join(c.dir, "voice.key")
	key, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) && create {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(key); err != nil {
			f.Close()
			return nil, err
		}
		return key, f.Close()
	}
	if err != nil {
		return nil, err
	}
	if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 || len(key) != 32 {
		return nil, fmt.Errorf("%s must be a 32-byte regular file private to its owner (0600)", path)
	}
	return key, nil
}

func voiceGCM(key []byte) (cipher.AEAD, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

// voiceTemplate opens approverID's template, made with the configured
// speaker model (another model's embeddings are not comparable).
func (c *ApprovalCore) voiceTemplate(ctx context.Context, approverID string) ([]float64, error) {
	var sealed, model string
	if err := c.db.QueryRowContext(ctx, `SELECT sealed, speaker_model FROM voice_templates WHERE approver_id = ?`, approverID).Scan(&sealed, &model); err != nil {
		return nil, err
	}
	if model != c.cfg.Voice.SpeakerModel {
		return nil, errors.New("voice template of another speaker model")
	}
	key, err := c.voiceKey(false)
	if err != nil {
		return nil, err
	}
	g, err := voiceGCM(key)
	if err != nil {
		return nil, err
	}
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < g.NonceSize() {
		return nil, errors.New("voice template unreadable")
	}
	plain, err := g.Open(nil, raw[:g.NonceSize()], raw[g.NonceSize():], []byte("apa-voice-template\x00"+approverID))
	if err != nil {
		return nil, errors.New("voice template unreadable")
	}
	var e []float64
	return e, json.Unmarshal(plain, &e)
}

// EnrollVoice makes approverID's speaker template from at least three
// recordings (WAVs, as the page sends them): the mean of their embeddings,
// sealed. It replaces an earlier template. The recordings are not kept.
func (c *ApprovalCore) EnrollVoice(ctx context.Context, approverID string, wavs [][]byte) (int, error) {
	v := c.cfg.Voice
	switch {
	case v == nil:
		return 0, errors.New("config: no voice section")
	case c.cfg.approver(approverID) == nil:
		return 0, Refusal("APPROVER_UNKNOWN")
	case len(wavs) < voiceMinSamples:
		return 0, fmt.Errorf("a voice template needs at least %d recordings", voiceMinSamples)
	}
	var mean []float64
	for i, w := range wavs {
		tctx, cancel := context.WithTimeout(ctx, 60*time.Second) // enrollment is the administrator's, not the approver's wait
		an, err := c.voice.analyze(tctx, w)
		cancel()
		if err != nil {
			return 0, fmt.Errorf("recording %d: %w", i+1, err)
		}
		if mean == nil {
			mean = make([]float64, len(an.Embedding))
		}
		if len(an.Embedding) != len(mean) {
			return 0, errors.New("the sidecar's embeddings differ in size")
		}
		var norm float64
		for _, x := range an.Embedding {
			norm += x * x
		}
		norm = math.Sqrt(norm)
		if !(norm > 0) || math.IsInf(norm, 0) { // zero, NaN or infinite: no template from it (review S3)
			return 0, fmt.Errorf("recording %d: an unusable embedding", i+1)
		}
		for j, x := range an.Embedding {
			mean[j] += x / norm / float64(len(wavs))
		}
	}
	plain, err := json.Marshal(mean)
	if err != nil {
		return 0, err
	}
	key, err := c.voiceKey(true)
	if err != nil {
		return 0, err
	}
	g, err := voiceGCM(key)
	if err != nil {
		return 0, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return 0, err
	}
	sealed := base64.StdEncoding.EncodeToString(g.Seal(nonce, nonce, plain, []byte("apa-voice-template\x00"+approverID)))
	_, err = c.db.ExecContext(ctx, `INSERT INTO voice_templates (approver_id, sealed, samples, speaker_model, created_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (approver_id) DO UPDATE SET sealed = excluded.sealed, samples = excluded.samples, speaker_model = excluded.speaker_model, created_at = excluded.created_at`,
		approverID, sealed, len(wavs), v.SpeakerModel, c.clock())
	return len(wavs), err
}

// DeleteVoice removes approverID's template; true when there was one.
func (c *ApprovalCore) DeleteVoice(ctx context.Context, approverID string) (bool, error) {
	res, err := c.db.ExecContext(ctx, `DELETE FROM voice_templates WHERE approver_id = ?`, approverID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// VoiceStats counts the stored voice outcomes by result and reason: the
// degradation rate's numbers (review S1).
func (c *ApprovalCore) VoiceStats(ctx context.Context) (map[string]int, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT result, coalesce(reason, ''), count(*) FROM voice_results GROUP BY result, reason ORDER BY result, reason`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var r, why string
		var n int
		if err := rows.Scan(&r, &why, &n); err != nil {
			return nil, err
		}
		out[strings.TrimSuffix(r+" "+why, " ")] = n
	}
	return out, rows.Err()
}

// voiceResultOf is the stored outcome of challenge id, if any.
func (c *ApprovalCore) voiceResultOf(ctx context.Context, challengeID string) (*VoiceResult, error) {
	var r VoiceResult
	var reason sql.NullString
	err := c.db.QueryRowContext(ctx, `SELECT result, reason FROM voice_results WHERE challenge_id = ?`, challengeID).Scan(&r.Result, &reason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	r.Reason = reason.String
	return &r, err
}
