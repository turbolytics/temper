// Package docs embeds the guide that `temper guide` prints, so the guide an
// LLM reads always matches the binary that is installed.
package docs

import _ "embed"

// Agents is docs/agents.md: how to work in a project that uses Temper.
//
//go:embed agents.md
var Agents string
