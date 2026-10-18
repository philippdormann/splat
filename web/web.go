// Package web embeds the server's HTML templates and static assets
// (CSS/JS/icons) into the binary so the deployed app needs no filesystem
// access to its own frontend and no CDN at runtime.
package web

import "embed"

//go:embed templates
var Templates embed.FS

//go:embed static
var Static embed.FS
