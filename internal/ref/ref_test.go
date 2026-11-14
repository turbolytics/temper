package ref

import (
	"testing"

	"github.com/turbolytics/temper"
)

func TestValidID(t *testing.T) {
	temper.Covers(t, "cli.check")
	for _, ok := range []string{"a", "sink.flush.keeps_batch", "a_1.b2"} {
		if !ValidID(ok) {
			t.Errorf("ValidID(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "A", "a..b", ".a", "a.", "a-b", "a/b", "a b"} {
		if ValidID(bad) {
			t.Errorf("ValidID(%q) = true, want false", bad)
		}
	}
}

func TestValidKey(t *testing.T) {
	temper.Covers(t, "cli.check")
	// Keys are the closed set's own names, so they may hold the spaces and
	// slashes real names do: Arrow's decimal(*, *), an interval[day/time].
	for _, ok := range []string{"int64", "list<int64>", "up.working", "a:b", "#x",
		"decimal(*, *)", "timestamp[us, tz=*]", "list<decimal(*, *)>", "interval[day/time]"} {
		if !ValidKey(ok) {
			t.Errorf("ValidKey(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", " a", "a ", "\ta", "a\tb", "a\nb", "a\u00a0", "\u00a0a"} {
		if ValidKey(bad) {
			t.Errorf("ValidKey(%q) = true, want false", bad)
		}
	}
}

func TestParse(t *testing.T) {
	temper.Covers(t, "matrix.exact_reference_only", temper.Subject("cli"))
	r := Parse("sink.types/list<int64>")
	if r != (Ref{Item: "sink.types", Member: "list<int64>"}) || !r.IsMember() || r.String() != "sink.types/list<int64>" {
		t.Fatalf("Parse member = %+v", r)
	}
	r = Parse("a.b")
	if r != (Ref{Item: "a.b"}) || r.IsMember() || r.String() != "a.b" {
		t.Fatalf("Parse item = %+v", r)
	}
	r = Parse("l/a/b")
	if r != (Ref{Item: "l", Member: "a/b"}) {
		t.Fatalf("Parse splits on the first slash: %+v", r)
	}
	// A key may hold a slash and spaces, because a lattice id holds neither.
	r = Parse("sink.types/interval[day/time]")
	if r != (Ref{Item: "sink.types", Member: "interval[day/time]"}) {
		t.Fatalf("Parse of a key with a slash: %+v", r)
	}
}
