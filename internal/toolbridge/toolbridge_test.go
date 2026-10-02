package toolbridge

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SIMPLYBOYS/agent-payment-authority/internal/contracts"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/merchant"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/policy"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/store"
	"github.com/SIMPLYBOYS/agent-payment-authority/internal/x402"
)

const t0 = 1790726450

var (
	sessA = contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-a"}
	sessB = contracts.Session{ConsumerID: "consumer-b", AgentID: "agent-b", TaskID: "task-b"}
)

// env is a real shop over HTTP and a real Store file. tamper, when set,
// answers a request instead of the shop.
type env struct {
	t       *testing.T
	now     atomic.Uint64
	reg     contracts.RegistryConfig
	profile policy.Profile
	srv     *httptest.Server
	shop    *merchant.Merchant
	tamper  atomic.Pointer[func(http.ResponseWriter, *http.Request) bool]
	path    string
	offers  store.OfferBook
	st      *store.Store
	sh      *Shop
}

func newEnv(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, path: filepath.Join(t.TempDir(), "db", "apa.db")}
	e.now.Store(t0)
	raw, err := os.ReadFile("../../testdata/contracts/registry.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := contracts.Decode(raw, &e.reg); err != nil {
		t.Fatal(err)
	}
	if raw, err = os.ReadFile("../../testdata/policy/m1-demo-profile.json"); err != nil {
		t.Fatal(err)
	}
	if e.profile, err = policy.LoadProfile(raw); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	e.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f := e.tamper.Load(); f != nil && (*f)(w, r) {
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(e.srv.Close)
	if e.shop, err = merchant.New(e.srv.URL); err != nil {
		t.Fatal(err)
	}
	e.shop.Register(mux)
	e.offers = store.OfferBook{}
	products := map[string]Product{}
	for _, o := range e.shop.Offers() {
		k := store.OfferKey{Method: o.Method, URL: o.URL}
		e.offers[k] = append(e.offers[k], o.Quote)
		products[o.ProductID] = Product{ID: o.ProductID, URL: o.URL}
	}
	e.open(e.offers)
	e.sh = &Shop{Store: e.st, CatalogURL: e.srv.URL + "/catalog", Products: products, Client: NewClient(), Now: e.now.Load}
	return e
}

func (e *env) open(offers store.OfferBook) {
	e.t.Helper()
	st, err := store.Open(store.Config{Path: e.path, Registry: e.reg, Offers: offers, Clock: e.now.Load})
	if err != nil {
		e.t.Fatal(err)
	}
	e.st = st
	if e.sh != nil {
		e.sh.Store = st
	}
	e.t.Cleanup(func() { st.Close() })
}

func (e *env) start(task string) *Binding {
	e.t.Helper()
	reg, _ := contracts.NewRegistry(e.reg)
	tk, w, err := reg.Binding(task)
	if err != nil {
		e.t.Fatal(err)
	}
	p := e.profile
	cred := policy.Credential{ConsumerID: tk.ConsumerID, ValidUntil: t0 + 86400, EvidenceMode: "fixture"}
	if err := e.st.StartTask(store.TaskSetup{TaskID: task, Profile: &p, Credential: &cred}); err != nil {
		e.t.Fatal(err)
	}
	return &Binding{Session: contracts.Session{ConsumerID: tk.ConsumerID, AgentID: tk.AgentID, TaskID: task}, WalletID: w.WalletID, From: w.From}
}

func (e *env) setTamper(f func(http.ResponseWriter, *http.Request) bool) { e.tamper.Store(&f) }

// required is the real 402 the shop serves, decoded, whatever tamper is set.
func (e *env) required() x402.PaymentRequired {
	e.t.Helper()
	saved := e.tamper.Swap(nil)
	defer e.tamper.Store(saved)
	resp, err := http.Get(e.srv.URL + "/resources/kr-market?edition=1")
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	var pr x402.PaymentRequired
	if err := x402.Decode(resp.Header.Get(x402.HeaderPaymentRequired), &pr); err != nil {
		e.t.Fatal(err)
	}
	return pr
}

// serve402 answers the resource with the shop's 402 after mut.
func (e *env) serve402(mut func(*x402.PaymentRequired)) {
	pr := e.required()
	mut(&pr)
	h, err := x402.Encode(pr)
	if err != nil {
		e.t.Fatal(err)
	}
	e.setTamper(func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/resources/kr-market" {
			return false
		}
		w.Header().Set(x402.HeaderPaymentRequired, h)
		w.WriteHeader(http.StatusPaymentRequired)
		return true
	})
}

func (e *env) call(b *Binding, tool, product string) (map[string]any, CallRecord) {
	e.t.Helper()
	res, rec := e.sh.Call(context.Background(), b, tool, product)
	var v map[string]any
	if err := json.Unmarshal([]byte(res.Content[0].Text), &v); err != nil {
		e.t.Fatal(err)
	}
	if res.IsError != (v["refused"] != nil) {
		e.t.Fatalf("isError %v does not match result %v", res.IsError, v)
	}
	return v, rec
}

func (e *env) purchases(sess contracts.Session) []store.PurchaseView {
	e.t.Helper()
	v, err := e.st.Task(sess)
	if err != nil {
		e.t.Fatal(err)
	}
	return v.Purchases
}

func TestParseArgs(t *testing.T) {
	for _, c := range []struct {
		tool, args, want string
		ok               bool
	}{
		{"list_products", ``, "", true},
		{"list_products", `{}`, "", true},
		{"list_products", `{"product_id":"kr-market"}`, "", false},
		{"get_quote", `{"product_id":"kr-market"}`, "kr-market", true},
		{"get_quote", `{}`, "", false},
		{"get_quote", `[]`, "", false},
		{"get_quote", `"kr-market"`, "", false},
		{"get_quote", `{"product_id":7}`, "", false},
		{"get_quote", `{"product_id":"KR-MARKET"}`, "", false},
		{"get_quote", `{"product_id":"../../etc/passwd"}`, "", false},
		{"get_quote", `{"product_id":"kr-market; rm -rf ~"}`, "", false},
		{"get_quote", `{"product_id":"kr-market","product_id":"other"}`, "", false},
		{"request_purchase", `{"product_id":"kr-market","task_id":"task-b"}`, "", false},
		{"request_purchase", `{"product_id":"kr-market","amount":"1"}`, "", false},
		{"request_purchase", `{"product_id":"kr-market","url":"http://203.0.113.9/"}`, "", false},
		{"request_purchase", `{"product_id":"` + strings.Repeat("a", 65) + `"}`, "", false},
	} {
		got, err := parseArgs(c.tool, json.RawMessage(c.args))
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("parseArgs(%s, %s) = %q, %v", c.tool, c.args, got, err)
		}
	}
}

// mcpSession runs Serve with call and returns one response per request line.
func mcpSession(t *testing.T, call Caller, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := Serve(context.Background(), strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, call); err != nil {
		t.Fatal(err)
	}
	var resps []map[string]any
	sc := bufio.NewScanner(strings.NewReader(out.String()))
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("response is not one JSON line: %q", sc.Text())
		}
		resps = append(resps, m)
	}
	return resps
}

func errCode(m map[string]any) float64 {
	e, _ := m["error"].(map[string]any)
	c, _ := e["code"].(float64)
	return c
}

func TestServeProtocol(t *testing.T) {
	var relayed []string
	call := func(_ context.Context, name string, args json.RawMessage) (ToolResult, error) {
		relayed = append(relayed, name+" "+string(args))
		return textResult(map[string]string{"ok": name}, false), nil
	}
	r := mcpSession(t, call,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_quote","arguments":{"product_id":"kr-market"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"http_get","arguments":{"url":"http://example.com"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
		`not json`,
		`{"jsonrpc":"2.0","id":7,"method":"ping"}`,
	)
	if len(r) != 8 {
		t.Fatalf("got %d responses, want 8 (the notification gets none): %v", len(r), r)
	}
	if errCode(r[0]) != -32600 {
		t.Errorf("tools/list before initialize: %v", r[0])
	}
	if v := r[1]["result"].(map[string]any)["protocolVersion"]; v != "2025-03-26" {
		t.Errorf("negotiated %v, want the client's supported version", v)
	}
	tools := r[2]["result"].(map[string]any)["tools"].([]any)
	var names []string
	for _, x := range tools {
		names = append(names, x.(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "list_products,get_quote,request_purchase" {
		t.Errorf("tools = %v", names)
	}
	if r[3]["result"].(map[string]any)["isError"] != false || len(relayed) != 1 || relayed[0] != `get_quote {"product_id":"kr-market"}` {
		t.Errorf("tools/call: %v, relayed %v", r[3], relayed)
	}
	if errCode(r[4]) != -32602 || errCode(r[5]) != -32601 || errCode(r[6]) != -32700 || r[6]["id"] != nil {
		t.Errorf("unknown tool %v, unknown method %v, parse error %v", r[4], r[5], r[6])
	}
	if r[7]["result"] == nil {
		t.Errorf("ping: %v", r[7])
	}
	// An unknown version gets the newest; a line over the limit ends the session.
	r = mcpSession(t, call, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"1999-01-01"}}`)
	if v := r[0]["result"].(map[string]any)["protocolVersion"]; v != protocolVersions[0] {
		t.Errorf("unknown version negotiated %v", v)
	}
	big := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"pad":"` + strings.Repeat("x", MaxMessageBytes) + `"}}`
	if err := Serve(context.Background(), strings.NewReader(big+"\n"), &strings.Builder{}, call); err != ErrTooLong {
		t.Errorf("oversized line: %v", err)
	}
	// A relay failure is an internal error, not a result.
	fail := func(context.Context, string, json.RawMessage) (ToolResult, error) { return ToolResult{}, os.ErrClosed }
	r = mcpSession(t, fail, `{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_products"}}`)
	if errCode(r[1]) != -32603 {
		t.Errorf("relay failure: %v", r[1])
	}
}

type recordExec struct{ calls atomic.Int32 }

func (x *recordExec) Call(_ context.Context, b *Binding, tool, product string) (ToolResult, CallRecord) {
	x.calls.Add(1)
	return textResult(map[string]string{"task": b.Session.TaskID}, false), CallRecord{Tool: tool, ProductID: product, Outcome: "ok"}
}

func refusedCode(t *testing.T, r ToolResult) string {
	t.Helper()
	var v map[string]any
	json.Unmarshal([]byte(r.Content[0].Text), &v)
	c, _ := v["refused"].(string)
	if (c != "") != r.IsError {
		t.Fatalf("refusal without isError: %v", r)
	}
	return c
}

func TestRelayIdentity(t *testing.T) {
	x := &recordExec{}
	srv, err := Listen(x)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if fi, err := os.Stat(filepath.Dir(srv.SocketPath())); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("socket directory: %v %v", fi, err)
	}
	ctx := context.Background()
	a := &Binding{Session: sessA}
	b := &Binding{Session: sessB}
	capA, _ := srv.Register(a)
	capB, _ := srv.Register(b)
	args := json.RawMessage(`{"product_id":"kr-market"}`)

	for c, want := range map[string]string{capA: "task-a", capB: "task-b"} {
		r, err := Dial(ctx, srv.SocketPath(), c, "get_quote", args)
		if err != nil || r.IsError || !strings.Contains(r.Content[0].Text, want) {
			t.Fatalf("legit call for %s: %v %v", want, r, err)
		}
	}
	// No capability, a guessed one, or one with a byte changed: no run.
	forged := []byte(capA)
	forged[0] ^= 1
	for _, c := range []string{"", "task-b", strings.Repeat("0", 64), string(forged)} {
		if r, _ := Dial(ctx, srv.SocketPath(), c, "get_quote", args); refusedCode(t, r) != "RUN_NOT_ACTIVE" {
			t.Errorf("capability %q: %v", c, r)
		}
	}
	// A revoked capability stays dead, including after a new run starts.
	srv.Revoke(capA)
	capA2, _ := srv.Register(&Binding{Session: sessA})
	if r, _ := Dial(ctx, srv.SocketPath(), capA, "request_purchase", args); refusedCode(t, r) != "RUN_NOT_ACTIVE" {
		t.Errorf("revoked capability replayed: %v", r)
	}
	if capA2 == capA {
		t.Fatal("capability reused")
	}
	// Unknown tools and bad arguments never reach the executor.
	before := x.calls.Load()
	for _, c := range []struct{ tool, args, code string }{
		{"http_get", `{"url":"http://example.com"}`, "UNKNOWN_TOOL"},
		{"get_quote", `{"product_id":"kr-market","wallet_id":"wallet-b"}`, "INVALID_ARGUMENTS"},
	} {
		if r, _ := Dial(ctx, srv.SocketPath(), capB, c.tool, json.RawMessage(c.args)); refusedCode(t, r) != c.code {
			t.Errorf("%s: %v", c.tool, r)
		}
	}
	if x.calls.Load() != before {
		t.Error("a refused call reached the executor")
	}
	// The trace never carries a name the model made up.
	for _, rec := range b.Calls() {
		if !known(rec.Tool) && rec.Tool != "unknown" {
			t.Errorf("traced tool name %q", rec.Tool)
		}
	}
	// The call budget is per run.
	for i := len(b.Calls()); i < MaxCalls; i++ {
		Dial(ctx, srv.SocketPath(), capB, "list_products", nil)
	}
	if r, _ := Dial(ctx, srv.SocketPath(), capB, "list_products", nil); refusedCode(t, r) != "TOO_MANY_CALLS" {
		t.Errorf("call %d: %v", MaxCalls+1, r)
	}
	if r, _ := Dial(ctx, srv.SocketPath(), capA2, "list_products", nil); r.IsError {
		t.Errorf("another run's budget was used: %v", r)
	}
	// A malformed relay request gets a refusal, not a crash.
	conn, err := net.Dial("unix", srv.SocketPath())
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("not json\n"))
	line, err := ReadLine(bufio.NewReader(conn), MaxMessageBytes)
	conn.Close()
	if err != nil || !strings.Contains(string(line), "BAD_REQUEST") {
		t.Errorf("malformed relay request: %q %v", line, err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(srv.SocketPath()); !os.IsNotExist(err) {
		t.Errorf("socket left behind: %v", err)
	}
}

func TestMainLineAsk(t *testing.T) {
	e := newEnv(t)
	b := e.start("task-a")
	list, rec := e.call(b, "list_products", "")
	items := list["products"].([]any)
	if rec.Outcome != "ok" || len(items) != 1 || items[0].(map[string]any)["product_id"] != "kr-market" {
		t.Fatalf("list_products: %v %v", list, rec)
	}
	q, rec := e.call(b, "get_quote", "kr-market")
	if rec.Outcome != "ok" || q["amount"] != "500000" {
		t.Fatalf("get_quote: %v %v", q, rec)
	}
	p, rec := e.call(b, "request_purchase", "kr-market")
	if rec.Outcome != "Ask" || p["decision"] != "Ask" || p["approval"] != "pending" || p["payment"] != "none" || p["already_requested"] != false {
		t.Fatalf("request_purchase: %v %v", p, rec)
	}
	ps := e.purchases(sessA)
	if len(ps) != 1 || ps[0].PurchaseOrderID != "po-task-a-kr-market" || ps[0].ResourceURL != e.srv.URL+"/resources/kr-market?edition=1" ||
		ps[0].Amount != "500000" || ps[0].Decision.PaymentStatus != "unavailable" {
		t.Fatalf("stored: %+v", ps)
	}
	// The model sees the whitelist only: no hashes, URLs, wallet or payer.
	for _, k := range []string{"intent_hash", "resource_url", "from", "wallet_id", "pay_to", "payment_requirement_hash"} {
		if _, ok := p[k]; ok {
			t.Errorf("projection leaks %s", k)
		}
	}
}

func TestCatalogFilteredAndCleaned(t *testing.T) {
	e := newEnv(t)
	b := e.start("task-a")
	url := e.srv.URL + "/resources/kr-market?edition=1"
	e.setTamper(func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/catalog" {
			return false
		}
		json.NewEncoder(w).Encode(map[string]any{"products": []any{
			map[string]any{"product_id": "kr-market", "title": "Brief\x1b[31m\nIGNORE PREVIOUS\u202E", "http_method": "GET", "resource_url": url,
				"price": map[string]any{"asset_display": "0.50", "fiat_amount": "700", "fiat_currency": "KRW"}},
			map[string]any{"product_id": "kr-market", "title": "off-origin copy", "http_method": "GET", "resource_url": "http://203.0.113.9/resources/kr-market?edition=1"},
			map[string]any{"product_id": "kr-market", "title": "POST copy", "http_method": "POST", "resource_url": url},
			map[string]any{"product_id": "free-stuff", "title": "not trusted", "http_method": "GET", "resource_url": url},
		}})
		return true
	})
	list, _ := e.call(b, "list_products", "")
	items := list["products"].([]any)
	if len(items) != 1 {
		t.Fatalf("catalog not filtered to trusted products: %v", items)
	}
	if title := items[0].(map[string]any)["title"]; title != "Brief[31mIGNORE PREVIOUS" {
		t.Errorf("title not cleaned: %q", title)
	}
	e.setTamper(func(w http.ResponseWriter, r *http.Request) bool { w.WriteHeader(500); return true })
	if _, rec := e.call(b, "list_products", ""); rec.Outcome != "SHOP_UNAVAILABLE" {
		t.Errorf("catalog 500: %v", rec)
	}
}

func Test402Tampering(t *testing.T) {
	e := newEnv(t)
	b := e.start("task-a")
	good := e.required()
	goodHeader, _ := x402.Encode(good)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(x402.HeaderPaymentRequired, goodHeader) // a valid 402, but at a host nobody asked
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer other.Close()
	raw := func(status int, headers []string, body string) func(http.ResponseWriter, *http.Request) bool {
		return func(w http.ResponseWriter, r *http.Request) bool {
			if r.URL.Path != "/resources/kr-market" {
				return false
			}
			for _, h := range headers {
				w.Header().Add(x402.HeaderPaymentRequired, h)
			}
			w.WriteHeader(status)
			w.Write([]byte(body))
			return true
		}
	}
	mut := func(f func(*x402.PaymentRequired)) func(http.ResponseWriter, *http.Request) bool {
		pr := e.required()
		f(&pr)
		h, err := x402.Encode(pr)
		if err != nil {
			t.Fatal(err)
		}
		return raw(http.StatusPaymentRequired, []string{h}, "")
	}
	for name, tamper := range map[string]func(http.ResponseWriter, *http.Request) bool{
		"resource url other query": mut(func(p *x402.PaymentRequired) { p.Resource.URL = e.srv.URL + "/resources/kr-market?edition=2" }),
		"resource url off origin":  mut(func(p *x402.PaymentRequired) { p.Resource.URL = "http://203.0.113.9/resources/kr-market?edition=1" }),
		"resource url trailing":    mut(func(p *x402.PaymentRequired) { p.Resource.URL += "&" }),
		"two offers":               mut(func(p *x402.PaymentRequired) { p.Accepts = append(p.Accepts, p.Accepts[0]) }),
		"x402 version 1":           mut(func(p *x402.PaymentRequired) { p.X402Version = 1 }),
		"redirect": func(w http.ResponseWriter, r *http.Request) bool {
			http.Redirect(w, r, other.URL+r.URL.RequestURI(), http.StatusFound)
			return true
		},
		"no header":          raw(http.StatusPaymentRequired, nil, ""),
		"two headers":        raw(http.StatusPaymentRequired, []string{goodHeader, goodHeader}, ""),
		"status 200":         raw(http.StatusOK, []string{goodHeader}, "content"),
		"not base64":         raw(http.StatusPaymentRequired, []string{"%%%"}, ""),
		"base64 not json":    raw(http.StatusPaymentRequired, []string{base64.StdEncoding.EncodeToString([]byte("not json"))}, ""),
		"body too large":     raw(http.StatusPaymentRequired, []string{goodHeader}, strings.Repeat("x", MaxBodyBytes+1)),
		"header too large":   raw(http.StatusPaymentRequired, []string{goodHeader, strings.Repeat("A", MaxMessageBytes)}, ""),
		"unknown json field": raw(http.StatusPaymentRequired, []string{base64.StdEncoding.EncodeToString([]byte(`{"x402Version":2,"resource":{"url":"` + good.Resource.URL + `"},"accepts":[` + string(good.Accepts[0]) + `],"evil":1}`))}, ""),
	} {
		e.setTamper(tamper)
		if _, rec := e.call(b, "get_quote", "kr-market"); rec.Outcome != "QUOTE_REFUSED" {
			t.Errorf("%s: get_quote %v", name, rec)
		}
		if _, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "QUOTE_REFUSED" {
			t.Errorf("%s: request_purchase %v", name, rec)
		}
		b = &Binding{Session: b.Session, WalletID: b.WalletID, From: b.From} // fresh call budget; same identity
	}
	if ps := e.purchases(sessA); len(ps) != 0 {
		t.Fatalf("a tampered 402 produced a purchase: %+v", ps)
	}

	// A well-formed 402 for the right URL whose terms the shop's
	// configuration does not offer passes the host's 402 checks, and the
	// Store refuses it: the offer book is configuration, not what arrived.
	e.serve402(func(p *x402.PaymentRequired) {
		var req map[string]any
		json.Unmarshal(p.Accepts[0], &req)
		req["amount"] = "400000"
		p.Accepts[0], _ = json.Marshal(req)
	})
	if q, rec := e.call(b, "get_quote", "kr-market"); rec.Outcome != "ok" || q["amount"] != "400000" {
		t.Fatalf("repriced quote: %v %v", q, rec)
	}
	if _, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "QUOTE_NOT_OFFERED" {
		t.Errorf("unoffered quote: %v", rec)
	}
	if ps := e.purchases(sessA); len(ps) != 0 {
		t.Fatalf("unoffered quote stored: %+v", ps)
	}
}

func TestPurchaseIntegrity(t *testing.T) {
	e := newEnv(t)
	b := e.start("task-a")
	if _, rec := e.call(b, "get_quote", "no-such-product"); rec.Outcome != "UNKNOWN_PRODUCT" {
		t.Errorf("unknown product: %v", rec)
	}
	first, rec := e.call(b, "request_purchase", "kr-market")
	if rec.Outcome != "Ask" {
		t.Fatal(rec)
	}
	// The same request again: the stored purchase is reported, nothing new
	// is submitted, even after the host clock moved.
	e.now.Add(5)
	again, rec := e.call(b, "request_purchase", "kr-market")
	if rec.Outcome != "Ask" || again["already_requested"] != true || again["decision"] != first["decision"] {
		t.Fatalf("retry: %v %v", again, rec)
	}
	events, _ := e.st.Events(sessA)
	decided := 0
	for _, ev := range events {
		if ev.EventType == "POLICY_DECIDED" {
			decided++
		}
	}
	if len(e.purchases(sessA)) != 1 || decided != 1 {
		t.Fatalf("one logical purchase expected: %d purchases, %d decisions", len(e.purchases(sessA)), decided)
	}

	// The assembled order is stable: same IDs and nonce on every assembly.
	// Resending it is a Store replay; the same key with other content is a
	// conflict, whatever field changed.
	pr := e.required()
	q, _ := pr.Quote(0)
	var req x402.Requirement
	json.Unmarshal(q.SelectedRequirement, &req)
	p := e.sh.Products["kr-market"]
	e.now.Store(t0) // the clock of the first submission
	o1, err := e.sh.assemble(b, p, "po-task-a-kr-market", q, req)
	if err != nil {
		t.Fatal(err)
	}
	stored := e.purchases(sessA)[0]
	if h, _ := o1.Intent.Hash(); o1.RequestID != stored.RequestID || o1.Intent.PurchaseOrderID != stored.PurchaseOrderID || h != stored.IntentHash {
		t.Fatalf("assembly not stable: %+v vs %+v", o1, stored)
	}
	for name, change := range map[string]func(*contracts.PurchaseOrder){
		"amount": func(o *contracts.PurchaseOrder) { o.Intent.Amount = "1" },
		"payer":  func(o *contracts.PurchaseOrder) { o.Intent.From = "0x2222222222222222222222222222222222222222" },
		"asset": func(o *contracts.PurchaseOrder) {
			o.Intent.AssetContract = "0x5555555555555555555555555555555555555555"
		},
		"hash": func(o *contracts.PurchaseOrder) {
			o.Intent.PaymentRequirementHash = "sha256:" + strings.Repeat("0", 64)
		},
		"window": func(o *contracts.PurchaseOrder) { o.Intent.ValidBefore += 99 },
	} {
		o := o1
		change(&o)
		raw, _ := json.Marshal(o)
		if _, err := e.st.Submit(b.Session, raw); err == nil {
			t.Errorf("%s changed under the same key was accepted", name)
		}
	}
	// A different task in the purchase is refused for this session.
	o := o1
	o.Intent.TaskID = "task-b"
	raw, _ := json.Marshal(o)
	if _, err := e.st.Submit(b.Session, raw); store.Code(err) == "" {
		t.Errorf("task swapped: %v", err)
	}
	if len(e.purchases(sessA)) != 1 {
		t.Fatal("a changed order was stored")
	}
}

func TestIdentityIsolation(t *testing.T) {
	e := newEnv(t)
	a := e.start("task-a")
	bb := e.start("task-b")
	if _, rec := e.call(a, "request_purchase", "kr-market"); rec.Outcome != "Ask" {
		t.Fatal(rec)
	}
	if _, rec := e.call(bb, "request_purchase", "kr-market"); rec.Outcome != "Ask" {
		t.Fatal(rec)
	}
	pa, pb := e.purchases(sessA), e.purchases(sessB)
	if len(pa) != 1 || len(pb) != 1 || pa[0].PurchaseOrderID == pb[0].PurchaseOrderID {
		t.Fatalf("A %+v B %+v", pa, pb)
	}
	// A binding that mixes A's caller with B's task or wallet is refused by
	// the Store and leaves B untouched. (The host never builds one; this is
	// the Store holding the line if it did.)
	next := e.start("task-a-next")
	for name, forged := range map[string]*Binding{
		"A claims B's task":   {Session: contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-b"}, WalletID: a.WalletID, From: a.From},
		"A claims B's wallet": {Session: next.Session, WalletID: bb.WalletID, From: bb.From},
		"A claims B's payer":  {Session: next.Session, WalletID: a.WalletID, From: bb.From},
	} {
		if _, rec := e.sh.Call(context.Background(), forged, "request_purchase", "kr-market"); rec.Outcome != "OWNERSHIP_MISMATCH" {
			t.Errorf("%s: %v", name, rec)
		}
	}
	if len(e.purchases(next.Session)) != 0 {
		t.Error("a forged binding stored a purchase")
	}
	if pb2 := e.purchases(sessB); len(pb2) != 1 || pb2[0].IntentHash != pb[0].IntentHash || pb2[0].ApprovalClosed != "" {
		t.Errorf("B changed: %+v", pb2)
	}
	if _, err := e.st.Task(contracts.Session{ConsumerID: "consumer-a", AgentID: "agent-a", TaskID: "task-b"}); store.Code(err) != "OWNERSHIP_MISMATCH" {
		t.Errorf("A read B's task: %v", err)
	}
}

func TestRestartAndRepricing(t *testing.T) {
	e := newEnv(t)
	b := e.start("task-a")
	if _, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "Ask" {
		t.Fatal(rec)
	}
	stored := e.purchases(sessA)[0]
	pr := e.required()
	q, _ := pr.Quote(0)
	var req x402.Requirement
	json.Unmarshal(q.SelectedRequirement, &req)
	orig, err := e.sh.assemble(b, e.sh.Products["kr-market"], stored.PurchaseOrderID, q, req)
	if err != nil {
		t.Fatal(err)
	}
	origRaw, _ := json.Marshal(orig) // same clock: the body the tool submitted

	// Restart with the shop repriced: the offer book no longer has the old quote.
	e.st.Close()
	repriced := store.OfferBook{}
	for k, qs := range e.offers {
		for _, q := range qs {
			var m map[string]any
			json.Unmarshal(q.SelectedRequirement, &m)
			m["amount"] = "600000"
			q.SelectedRequirement, _ = json.Marshal(m)
			repriced[k] = append(repriced[k], q)
		}
	}
	e.open(repriced)
	// The stored purchase is still the Store's: the tool reports it, and the
	// original body resent under the same session is a replay.
	if v, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "Ask" || v["already_requested"] != true {
		t.Fatalf("after restart: %v %v", v, rec)
	}
	res, err := e.st.Submit(b.Session, origRaw)
	if err != nil || !res.Replay || res.Purchase.IntentHash != stored.IntentHash {
		t.Fatalf("original resend after repricing: %+v %v", res, err)
	}
	// A new purchase cannot ride the old quote past the new offer book.
	next := e.start("task-a-next")
	if _, rec := e.call(next, "request_purchase", "kr-market"); rec.Outcome != "QUOTE_NOT_OFFERED" {
		t.Errorf("new purchase on the old quote: %v", rec)
	}
}

func TestClean(t *testing.T) {
	if got := Clean("a\nb\x1b[0m\u202Ec\xff", 100); got != "ab[0mc�" {
		t.Errorf("Clean = %q", got)
	}
	if got := Clean(strings.Repeat("é", 10), 3); got != "ééé" {
		t.Errorf("Clean cap = %q", got)
	}
}

// A request that arrives after the task ended (a late or in-flight call) is
// the Store's to refuse; no decision follows a task's end.
func TestPurchaseAfterEnd(t *testing.T) {
	e := newEnv(t)
	for task, end := range map[string]func(string) error{"task-a": e.st.Cancel, "task-b": e.st.CLITimeout} {
		b := e.start(task)
		if err := end(task); err != nil {
			t.Fatal(err)
		}
		if _, rec := e.call(b, "request_purchase", "kr-market"); rec.Outcome != "TASK_NOT_ACTIVE" {
			t.Errorf("%s: %v", task, rec)
		}
		if len(e.purchases(b.Session)) != 0 {
			t.Errorf("%s: purchase stored after the end", task)
		}
	}
}
