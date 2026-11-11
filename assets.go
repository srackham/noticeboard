package main

import "embed"

// assetFS holds the templates and static files compiled into the binary.
//
//go:embed templates static
var assetFS embed.FS
