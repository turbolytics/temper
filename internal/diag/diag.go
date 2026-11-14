// Package diag is the one shape every Temper error takes: the file, the id,
// what is wrong, and the rule it breaks, in a line an agent can act on.
package diag

import (
	"sort"
	"strings"
)

// Error is one finding.
type Error struct {
	File    string // relative to the project root, forward slashes
	ID      string // the id the finding is about, if any
	Message string
	Rule    string // e.g. "R6 gap", "§14.3"; empty when none applies
}

// String renders `<file>: <id> <message> (<rule>)`, leaving out the id and
// the rule when they are empty.
func (e Error) String() string {
	var b strings.Builder
	b.WriteString(e.File)
	b.WriteString(": ")
	if e.ID != "" {
		b.WriteString(e.ID)
		b.WriteByte(' ')
	}
	b.WriteString(e.Message)
	if e.Rule != "" {
		b.WriteString(" (")
		b.WriteString(e.Rule)
		b.WriteByte(')')
	}
	return b.String()
}

// List is a set of findings.
type List []Error

// Sorted returns a copy ordered by file, id, then message.
func (l List) Sorted() List {
	out := append(List(nil), l...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.File != b.File {
			return a.File < b.File
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Message < b.Message
	})
	return out
}
