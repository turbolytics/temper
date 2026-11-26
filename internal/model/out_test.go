package model

import (
	"testing"

	"github.com/turbolytics/temper"
)

func wantPath(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOutRootRedirectsGeneratedPaths(t *testing.T) {
	temper.Covers(t, "cli.out")
	p := &Project{Root: "/proj", Config: Config{Dir: "temper", Matrix: "MATRIX.md", Evidence: ".temper/evidence"}}
	wantPath(t, p.StatusDir(), "/proj/temper/status")
	wantPath(t, p.MatrixPath(), "/proj/MATRIX.md")
	wantPath(t, p.EvidenceDir(), "/proj/.temper/evidence")
	wantPath(t, p.ScenarioLogDir(), "/proj/.temper/scenarios")

	p.Out = "/res/pg_duckdb"
	wantPath(t, p.OutRoot(), "/res/pg_duckdb")
	wantPath(t, p.StatusDir(), "/res/pg_duckdb/temper/status")
	wantPath(t, p.MatrixPath(), "/res/pg_duckdb/MATRIX.md")
	wantPath(t, p.EvidenceDir(), "/res/pg_duckdb/.temper/evidence")
	wantPath(t, p.ScenarioLogDir(), "/res/pg_duckdb/.temper/scenarios")

	p.Config.Evidence = "/abs/ev"
	wantPath(t, p.EvidenceDir(), "/abs/ev")
}
