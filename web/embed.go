// Package web holds the demo Dashboard (B1-06 delivery 2, docs/demo-ui.md):
// plain HTML, CSS and JavaScript modules, no framework, bundler or CDN.
// cmd/demo serves exactly the files embedded here and nothing else; the
// tests under web/test are not embedded.
package web

import "embed"

//go:embed index.html host.html enroll.html style.css app.js adapter.js render.js unity.js host.js hostcreds.js scene.js enroll.js approve.js voice.js show.html show.js showlink.js pixelscale.js
var FS embed.FS

// Assets are the files served under /ui/, by name, with their media type.
var Assets = map[string]string{
	"style.css":     "text/css; charset=utf-8",
	"app.js":        "text/javascript; charset=utf-8",
	"adapter.js":    "text/javascript; charset=utf-8",
	"render.js":     "text/javascript; charset=utf-8",
	"unity.js":      "text/javascript; charset=utf-8",
	"host.js":       "text/javascript; charset=utf-8",
	"hostcreds.js":  "text/javascript; charset=utf-8",
	"scene.js":      "text/javascript; charset=utf-8",
	"enroll.js":     "text/javascript; charset=utf-8",
	"approve.js":    "text/javascript; charset=utf-8",
	"voice.js":      "text/javascript; charset=utf-8",
	"show.js":       "text/javascript; charset=utf-8",
	"showlink.js":   "text/javascript; charset=utf-8",
	"pixelscale.js": "text/javascript; charset=utf-8",
}
