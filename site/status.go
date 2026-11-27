package main

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// The findings status check records, for each finding's GitHub URL, only
// the fields that decide its status. Comparing two snapshots field by field
// says exactly what changed, which GitHub's updated_at cannot: our own
// edits, reactions and bot comments move that too.

// Comment is the newest comment by someone other than us or a bot.
type Comment struct {
	ID          string `json:"id"`     // REST ids as decimal strings, GraphQL node ids as given
	Author      string `json:"author"` // "ghost" for a deleted account
	Association string `json:"association"`
	Date        string `json:"date"`
}

// LinkedPR is a pull request that mentions the finding.
type LinkedPR struct {
	Ref   string `json:"ref"`   // "owner/repo#n"
	State string `json:"state"` // open, draft, closed or merged
}

// Entry is one URL's recorded state.
type Entry struct {
	State          string     `json:"state"` // open or closed; merged for a merged PR
	StateReason    string     `json:"state_reason,omitempty"`
	Closed         string     `json:"closed,omitempty"`
	ClosedBy       string     `json:"closed_by,omitempty"`
	Merged         string     `json:"merged,omitempty"`
	Draft          bool       `json:"draft,omitempty"`
	Answered       bool       `json:"answered,omitempty"`
	Labels         []string   `json:"labels,omitempty"`
	LinkedPRs      []LinkedPR `json:"linked_prs,omitempty"`
	OthersComments int        `json:"others_comments"`
	Newest         *Comment   `json:"newest,omitempty"`
	NewestBody     string     `json:"-"` // for the report, never stored
	newestTime     string     // the newest comment's full timestamp, to compare
}

// Snapshot is every finding's entry, keyed by the row's URL.
type Snapshot map[string]Entry

// Change is one field that differs between two snapshots.
type Change struct{ URL, Field, Old, New string }

// fields flattens an entry into the named values Diff compares.
func fields(e Entry) map[string]string {
	e = sorted(e)
	prs := make([]string, len(e.LinkedPRs))
	for i, p := range e.LinkedPRs {
		prs[i] = p.Ref + " " + p.State
	}
	newest := ""
	if e.Newest != nil {
		newest = e.Newest.ID
	}
	return map[string]string{
		"state":           e.State,
		"state_reason":    e.StateReason,
		"closed":          e.Closed,
		"closed_by":       e.ClosedBy,
		"merged":          e.Merged,
		"draft":           strconv.FormatBool(e.Draft),
		"answered":        strconv.FormatBool(e.Answered),
		"labels":          strings.Join(e.Labels, ", "),
		"linked_prs":      strings.Join(prs, ", "),
		"others_comments": strconv.Itoa(e.OthersComments),
		"newest":          newest,
	}
}

// Diff lists the fields that changed from old to cur, sorted by URL, then
// field. Without an old snapshot the run is a baseline and nothing changed.
// A URL new to a non-empty snapshot is one change, field "new".
func Diff(old, cur Snapshot) []Change {
	if old == nil {
		return nil
	}
	var out []Change
	for url, e := range cur {
		prev, ok := old[url]
		if !ok {
			out = append(out, Change{url, "new", "", e.State})
			continue
		}
		a, b := fields(prev), fields(e)
		for name, v := range b {
			if a[name] != v {
				out = append(out, Change{url, name, a[name], v})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].URL != out[j].URL {
			return out[i].URL < out[j].URL
		}
		return out[i].Field < out[j].Field
	})
	return out
}

// Propose gives the status a rule decides for a findings.csv kind, or false
// when deciding needs judgment: an issue closed as not planned, a maintainer
// comment, a Discussion reply, or a fix that may cover only one item.
func Propose(kind string, e Entry) (string, bool) {
	switch kind {
	case "fix-pr", "test-pr", "temper-pr":
		switch {
		case e.State == "merged":
			return "merged", true
		case e.State == "closed":
			return "closed", true
		case e.Draft:
			return "draft", true
		default:
			return "open", true
		}
	case "issue":
		if e.State != "closed" {
			return "", false
		}
		if e.StateReason == "duplicate" {
			return "duplicate", true
		}
		if e.StateReason == "completed" {
			for _, p := range e.LinkedPRs {
				if p.State == "merged" {
					return "fixed", true
				}
			}
		}
	}
	return "", false
}

// sorted returns a copy of e with its labels and linked PRs sorted.
func sorted(e Entry) Entry {
	e.Labels = append([]string(nil), e.Labels...)
	sort.Strings(e.Labels)
	e.LinkedPRs = append([]LinkedPR(nil), e.LinkedPRs...)
	sort.Slice(e.LinkedPRs, func(i, j int) bool { return e.LinkedPRs[i].Ref < e.LinkedPRs[j].Ref })
	if len(e.Labels) == 0 {
		e.Labels = nil
	}
	if len(e.LinkedPRs) == 0 {
		e.LinkedPRs = nil
	}
	return e
}

// EncodeSnapshot writes s the same way every time: lists sorted, keys sorted
// by encoding/json, two-space indent and a trailing newline.
func EncodeSnapshot(s Snapshot) ([]byte, error) {
	out := make(Snapshot, len(s))
	for url, e := range s {
		out[url] = sorted(e)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// DecodeSnapshot reads a snapshot EncodeSnapshot wrote.
func DecodeSnapshot(b []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s == nil {
		s = Snapshot{}
	}
	return s, nil
}
