package status

import (
	"testing"

	"github.com/turbolytics/temper"
)

func TestSet_PutLookup(t *testing.T) {
	temper.Covers(t, "cli.matrix")
	s := NewSet()
	s.Put("feature", "sink.kafka", "", "", "unit", Covered)
	s.Put("invariant", "sink.flush.keeps_batch", "", "sink.kafka", "integration", Missing)
	s.Put("lattice", "sink.types", "int64", "sink.kafka", "integration", Exempt)
	s.Put("lattice", "heuristics", "up.working", "", "unit", Failing)

	if s.Features["sink.kafka"]["unit"] != Covered ||
		s.Subjects["sink.kafka"]["sink.flush.keeps_batch"]["integration"] != Missing ||
		s.Subjects["sink.kafka"]["sink.types/int64"]["integration"] != Exempt ||
		s.Lattices["heuristics"]["up.working"]["unit"] != Failing {
		t.Fatalf("Put addressed the wrong rows: %+v", s)
	}
	for _, c := range []struct {
		kind, item, member, subject, level string
		want                               Status
	}{
		{"feature", "sink.kafka", "", "", "unit", Covered},
		{"invariant", "sink.flush.keeps_batch", "", "sink.kafka", "integration", Missing},
		{"lattice", "sink.types", "int64", "sink.kafka", "integration", Exempt},
		{"lattice", "heuristics", "up.working", "", "unit", Failing},
	} {
		if got, ok := s.Lookup(c.kind, c.item, c.member, c.subject, c.level); !ok || got != c.want {
			t.Errorf("Lookup(%v) = %q, %v; want %q", c, got, ok, c.want)
		}
	}
	if _, ok := s.Lookup("feature", "sink.kafka", "", "", "integration"); ok {
		t.Error("Lookup found a level never put")
	}
	if _, ok := s.Lookup("invariant", "x", "", "nobody", "unit"); ok {
		t.Error("Lookup found a subject never put")
	}
	if Covered != "covered" || Exempt != "exempt" || Failing != "failing" || Missing != "missing" || Unenforced != "unenforced" || NotRequired != "not_required" {
		t.Error("status strings changed; they are committed and must stay exact")
	}
}
