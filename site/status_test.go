package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

const statusURL = "https://github.com/fsspec/filesystem_spec/issues/2470"

// A linked fix PR merges while its issue stays open (fsspec#2470 item 4):
// the diff names the field and both values.
func TestDiffNamesTheFieldAndBothValues(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	old := Snapshot{statusURL: {State: "open", LinkedPRs: []LinkedPR{{"fsspec/filesystem_spec#2481", "open"}}}}
	cur := Snapshot{statusURL: {State: "open", LinkedPRs: []LinkedPR{{"fsspec/filesystem_spec#2481", "merged"}}}}
	got := Diff(old, cur)
	want := []Change{{statusURL, "linked_prs", "fsspec/filesystem_spec#2481 open", "fsspec/filesystem_spec#2481 merged"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff = %#v\nwant %#v", got, want)
	}
}

func TestDiffWithoutASnapshotIsABaseline(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	cur := Snapshot{statusURL: {State: "open", OthersComments: 3}}
	if got := Diff(nil, cur); len(got) != 0 {
		t.Fatalf("a first run reports no changes, got %#v", got)
	}
}

func TestDiffReportsANewURL(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	u2 := "https://github.com/acme/db/issues/7"
	old := Snapshot{statusURL: {State: "open"}}
	cur := Snapshot{statusURL: {State: "open"}, u2: {State: "open"}}
	got := Diff(old, cur)
	want := []Change{{u2, "new", "", "open"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff = %#v\nwant %#v", got, want)
	}
}

func TestDiffSortsByURLThenField(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	a, b := "https://github.com/a/a/issues/1", "https://github.com/b/b/issues/1"
	old := Snapshot{a: {State: "open"}, b: {State: "open"}}
	cur := Snapshot{
		b: {State: "closed", StateReason: "completed"},
		a: {State: "open", OthersComments: 1, Newest: &Comment{ID: "9", Author: "maint", Association: "MEMBER", Date: "2026-11-26"}},
	}
	var got []string
	for _, c := range Diff(old, cur) {
		got = append(got, c.URL+" "+c.Field+" "+c.Old+"→"+c.New)
	}
	want := []string{
		a + " newest →9",
		a + " others_comments 0→1",
		b + " state open→closed",
		b + " state_reason →completed",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Diff =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestProposeDecidesOnlyMechanicalStatuses(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	merged := []LinkedPR{{"acme/db#9", "merged"}}
	cases := []struct {
		name, kind string
		e          Entry
		status     string
		ok         bool
	}{
		{"merged PR", "fix-pr", Entry{State: "merged"}, "merged", true},
		{"closed PR", "fix-pr", Entry{State: "closed"}, "closed", true},
		{"draft PR", "fix-pr", Entry{State: "open", Draft: true}, "draft", true},
		{"open PR", "temper-pr", Entry{State: "open"}, "open", true},
		{"duplicate issue", "issue", Entry{State: "closed", StateReason: "duplicate"}, "duplicate", true},
		{"issue closed by a merged PR", "issue", Entry{State: "closed", StateReason: "completed", LinkedPRs: merged}, "fixed", true},
		{"issue closed without a merged PR", "issue", Entry{State: "closed", StateReason: "completed"}, "", false},
		{"issue closed as not planned", "issue", Entry{State: "closed", StateReason: "not_planned"}, "", false},
		{"open issue with a merged PR", "issue", Entry{State: "open", LinkedPRs: merged}, "", false},
		{"Discussion reply", "report", Entry{State: "open", OthersComments: 1}, "", false},
	}
	for _, c := range cases {
		status, ok := Propose(c.kind, c.e)
		if status != c.status || ok != c.ok {
			t.Errorf("%s: Propose = (%q, %v), want (%q, %v)", c.name, status, ok, c.status, c.ok)
		}
	}
}

func TestSnapshotEncodingIsStable(t *testing.T) {
	temper.Covers(t, "site.findings_status")
	s := Snapshot{statusURL: {
		State:      "open",
		Labels:     []string{"b", "a"},
		LinkedPRs:  []LinkedPR{{"z/z#2", "open"}, {"a/a#1", "merged"}},
		Newest:     &Comment{ID: "9", Author: "maint", Association: "MEMBER", Date: "2026-11-26"},
		NewestBody: "never stored",
	}}
	one, err := EncodeSnapshot(s)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := EncodeSnapshot(s)
	if !bytes.Equal(one, two) {
		t.Fatal("two encodings of one snapshot differ")
	}
	if !bytes.HasSuffix(one, []byte("\n")) {
		t.Fatal("snapshot.json lacks a trailing newline")
	}
	if bytes.Contains(one, []byte("never stored")) {
		t.Fatal("the newest comment's body was stored")
	}
	text := string(one)
	if strings.Index(text, `"a"`) > strings.Index(text, `"b"`) || strings.Index(text, "a/a#1") > strings.Index(text, "z/z#2") {
		t.Fatalf("labels and linked PRs are not sorted:\n%s", text)
	}
	back, err := DecodeSnapshot(one)
	if err != nil {
		t.Fatal(err)
	}
	e := back[statusURL]
	if !reflect.DeepEqual(e.Labels, []string{"a", "b"}) || e.LinkedPRs[0].Ref != "a/a#1" || e.Newest.Author != "maint" {
		t.Fatalf("decoded entry = %#v", e)
	}
	if s[statusURL].Labels[0] != "b" {
		t.Fatal("encoding sorted the caller's slice in place")
	}
}
