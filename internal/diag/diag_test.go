package diag

import (
	"testing"

	"github.com/turbolytics/temper"
)

func TestError_String(t *testing.T) {
	temper.Covers(t, "cli.check")
	cases := []struct {
		e    Error
		want string
	}{
		{
			Error{File: "temper/subjects/sink.postgres.yml", ID: "sink.postgres", Message: "does not answer sink.types/timestamp_us_utc", Rule: "R6 gap"},
			"temper/subjects/sink.postgres.yml: sink.postgres does not answer sink.types/timestamp_us_utc (R6 gap)",
		},
		{Error{File: "temper.yml", Message: "no levels declared", Rule: "§6"}, "temper.yml: no levels declared (§6)"},
		{Error{File: "temper.yml", ID: "x", Message: "is odd"}, "temper.yml: x is odd"},
	}
	for _, c := range cases {
		if got := c.e.String(); got != c.want {
			t.Errorf("String() = %q, want %q", got, c.want)
		}
	}
}

func TestList_Sorted(t *testing.T) {
	temper.Covers(t, "cli.check")
	l := List{
		{File: "b", ID: "a", Message: "m"},
		{File: "a", ID: "b", Message: "m"},
		{File: "a", ID: "a", Message: "z"},
		{File: "a", ID: "a", Message: "m"},
	}
	got := l.Sorted()
	want := List{
		{File: "a", ID: "a", Message: "m"},
		{File: "a", ID: "a", Message: "z"},
		{File: "a", ID: "b", Message: "m"},
		{File: "b", ID: "a", Message: "m"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Sorted() = %v, want %v", got, want)
		}
	}
}
