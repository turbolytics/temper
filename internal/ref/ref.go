// Package ref holds the grammar of ids, lattice keys and references.
package ref

import (
	"regexp"
	"strings"
	"unicode"
)

var idPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

// ValidID reports whether s is a well-formed id (spec §5).
func ValidID(s string) bool { return idPattern.MatchString(s) }

// ValidKey reports whether s can be a lattice member key: non-empty, no
// whitespace, no slash. Keys are looser than ids (`list<int64>`).
func ValidKey(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r == '/' || unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// Ref is a reference to a declared item, or to a member of a lattice.
type Ref struct {
	Item   string
	Member string // empty unless the reference is `lattice/key`
}

// Parse splits a reference on its first slash.
func Parse(s string) Ref {
	item, member, _ := strings.Cut(s, "/")
	return Ref{Item: item, Member: member}
}

// IsMember reports whether r names a lattice member.
func (r Ref) IsMember() bool { return r.Member != "" }

func (r Ref) String() string {
	if r.Member == "" {
		return r.Item
	}
	return r.Item + "/" + r.Member
}
