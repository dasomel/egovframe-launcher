// Package web embeds the launcher's static UI assets.
package web

import "embed"

//go:embed index.html style.css app.js
var files embed.FS

// Assets is the embedded UI filesystem served at "/".
var Assets = files
