package signerclient

// The Host's client of serve-approvals (A2; docs/commercial-human-
// approval-design.md 6.1, 7). It only calls: the Store's attempts and their
// recovery are the Host's (cmd/demo). Every answer is one of four kinds,
// and a failed or unreadable call is never taken for "not found".

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/signer"
)

// Kind of a Signer answer.
const (
	KindOK       = "ok"        // 200
	KindRefused  = "refused"   // 409 or 400 with a fixed code
	KindNotFound = "not_found" // 404: the read succeeded and found nothing
	KindUnknown  = "unknown"   // no answer, a 5xx or an unreadable one
)

// Answer is one call's outcome; Code is the Signer's fixed code.
type Answer struct {
	Kind  string
	Code  string
	Reply signer.ApprovalReply
}

// ApprovalsTimeout bounds one call to serve-approvals: longer than its own
// limit with a voice challenge (5 s plus the sidecar's, at most 10 s; V1).
const ApprovalsTimeout = 20 * time.Second

// Approvals calls serve-approvals on its Unix socket.
type Approvals struct {
	http *http.Client
}

func NewApprovals(socket string) *Approvals {
	return &Approvals{http: &http.Client{Timeout: ApprovalsTimeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		}}}}
}

func (c *Approvals) do(method, path string, body any) Answer {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return Answer{Kind: KindUnknown}
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "http://signer"+path, rd)
	if err != nil {
		return Answer{Kind: KindUnknown}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Answer{Kind: KindUnknown}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var r signer.ApprovalReply
	if err != nil || json.Unmarshal(raw, &r) != nil {
		return Answer{Kind: KindUnknown}
	}
	switch {
	case resp.StatusCode == http.StatusOK:
		return Answer{Kind: KindOK, Reply: r}
	case resp.StatusCode == http.StatusNotFound && r.Error != "":
		return Answer{Kind: KindNotFound, Code: r.Error, Reply: r}
	case (resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusBadRequest) && r.Error != "":
		return Answer{Kind: KindRefused, Code: r.Error, Reply: r}
	}
	return Answer{Kind: KindUnknown, Code: r.Error}
}

func (c *Approvals) Challenge(r signer.ChallengeRequest) Answer {
	return c.do("POST", "/v1/approvals/challenge", r)
}

func (c *Approvals) ChallengeByRequest(id string) Answer {
	return c.do("GET", "/v1/approvals/challenge?request_id="+url.QueryEscape(id), nil)
}

func (c *Approvals) ChallengeByID(id string) Answer {
	return c.do("GET", "/v1/approvals/challenge?challenge_id="+url.QueryEscape(id), nil)
}

func (c *Approvals) Close(challengeID, closeRequestID string) Answer {
	return c.do("POST", "/v1/approvals/challenges/"+url.PathEscape(challengeID)+"/close", map[string]string{"close_request_id": closeRequestID})
}

func (c *Approvals) Verify(r signer.VerifyRequest) Answer {
	return c.do("POST", "/v1/approvals/verify", r)
}

func (c *Approvals) ByVerifyRequest(id string) Answer {
	return c.do("GET", "/v1/approvals?verify_request_id="+url.QueryEscape(id), nil)
}

func (c *Approvals) Approval(id string) Answer {
	return c.do("GET", "/v1/approvals/"+url.PathEscape(id), nil)
}

func (c *Approvals) Abandon(id string) Answer {
	return c.do("POST", "/v1/approvals/"+url.PathEscape(id)+"/abandon", nil)
}

func (c *Approvals) Health() Answer { return c.do("GET", "/v1/health", nil) }

// EnrollRelay sends the enrollment page's request on as it is: the
// options (code only) or the new credential (code and credential).
func (c *Approvals) EnrollRelay(id string, body map[string]any) (int, json.RawMessage) {
	path := "/v1/enrollments/" + url.PathEscape(id)
	if _, ok := body["credential"]; !ok {
		path += "/options"
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", "http://signer"+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || !json.Valid(out) {
		return 0, nil
	}
	return resp.StatusCode, out
}
