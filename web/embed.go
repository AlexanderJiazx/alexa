// Package web embeds the built React frontend.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
