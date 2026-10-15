package boox

import "embed"

// templateFS holds the BOOX views and the text written onto tablets, kept out
// of Go source so their wording can be translated and reviewed as files.
//
//go:embed templates
var templateFS embed.FS
