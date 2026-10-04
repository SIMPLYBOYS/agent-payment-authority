package main

import (
	"net/http"

	"github.com/SIMPLYBOYS/agent-payment-authority/web"
)

// The Dashboard (docs/demo-ui.md): the page at /, its listed assets under
// /ui/, and GET /api/viewer. Nothing else is served from disk: the files
// are embedded, so no request can reach the repository, var/, the
// database, the token file or a trace.

// pageHeaders allow only this origin's own scripts, styles, API and Unity
// build, no framing, no forms and no referrer. 'wasm-unsafe-eval' lets the
// Unity build compile its WebAssembly; it does not allow JavaScript eval.
func pageHeaders(h http.Header, contentType string) {
	h.Set("Content-Type", contentType)
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self' blob: data:; connect-src 'self'; worker-src 'self' blob:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
}

func (a *api) registerUI(mux *http.ServeMux) {
	mux.HandleFunc("GET /{$}", a.page)
	mux.HandleFunc("GET /ui/{name}", a.asset)
	mux.HandleFunc("GET /unity/{name}", a.unityFile)
	mux.HandleFunc("GET /api/viewer", a.viewer)
}

// unityFiles are the Web build's files the page loads, by name; nothing
// else of the build directory, and nothing of the Unity project, is served.
var unityFiles = map[string]string{
	"WebGL.loader.js":    "text/javascript; charset=utf-8",
	"WebGL.framework.js": "text/javascript; charset=utf-8",
	"WebGL.wasm":         "application/wasm",
	"WebGL.data":         "application/octet-stream",
}

// unityFile serves one listed build file from the build directory, opened
// as an os.Root so no name or link can leave it. Without a build the page
// says "Unity 場景未就緒"; nothing replaces the shop.
func (a *api) unityFile(w http.ResponseWriter, r *http.Request) {
	if !a.hostOK(w, r) {
		return
	}
	name := r.PathValue("name")
	ct, ok := unityFiles[name]
	if !ok || a.unity == nil {
		http.NotFound(w, r)
		return
	}
	f, err := a.unity.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	pageHeaders(w.Header(), ct)
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

// hostOK applies the API's Host rule to the page and its assets too.
func (a *api) hostOK(w http.ResponseWriter, r *http.Request) bool {
	if r.Host != a.addr {
		fail(w, http.StatusForbidden, "forbidden_host")
		return false
	}
	return true
}

func (a *api) page(w http.ResponseWriter, r *http.Request) {
	if !a.hostOK(w, r) {
		return
	}
	body, err := web.FS.ReadFile("index.html")
	if err != nil {
		fail(w, http.StatusInternalServerError, "page_unavailable")
		return
	}
	pageHeaders(w.Header(), "text/html; charset=utf-8")
	w.Write(body)
}

func (a *api) asset(w http.ResponseWriter, r *http.Request) {
	if !a.hostOK(w, r) {
		return
	}
	name := r.PathValue("name")
	ct, ok := web.Assets[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	body, err := web.FS.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pageHeaders(w.Header(), ct)
	w.Write(body)
}

// viewerReply is who a viewer token is: its role and consumer, nothing
// about other consumers, tokens or the registry.
type viewerReply struct {
	Role       string `json:"role"`
	ConsumerID string `json:"consumer_id"`
	Note       string `json:"note"`
}

// viewer answers GET /api/viewer for viewer tokens only: the host token is
// refused (the Dashboard never holds it), and an MCP capability is not a
// token at all (401).
func (a *api) viewer(w http.ResponseWriter, r *http.Request) {
	g, ok := a.authorize(w, r)
	if !ok {
		return
	}
	if g.Role != "viewer" {
		fail(w, http.StatusForbidden, "viewer_only")
		return
	}
	reply(w, http.StatusOK, viewerReply{Role: g.Role, ConsumerID: g.ConsumerID,
		Note: "Local demo capability for this run of the demo host; not a user login."})
}
