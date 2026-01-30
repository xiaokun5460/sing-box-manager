package api

import (
	_ "embed"
)

//go:embed web/index.html
var indexHTML string

//go:embed web/app.js
var appJS string
