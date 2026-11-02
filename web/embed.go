// Package web embeds the dashboard's static assets into the acc binary.
package web

import "embed"

//go:embed dist
var FS embed.FS
