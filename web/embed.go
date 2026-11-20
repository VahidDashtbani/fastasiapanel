package webui

import "embed"

//go:embed index.html login.html app.js login.js style.css
var FS embed.FS
