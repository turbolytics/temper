package main

import (
	"os"
	"testing"

	"github.com/turbolytics/temper"
)

// The tests run commands in-process, and --out defaults to $TEMPER_OUT. A
// TEMPER_OUT exported for other work (results kept in another repository)
// must neither redirect the fixtures' output nor change any verdict here.
// The run tests' helper subprocess (TestHelperEnv) keeps it: reporting the
// TEMPER_OUT that temper run gave it is its job.
func init() {
	if os.Getenv("TEMPER_HELPER_OUT") == "" {
		os.Unsetenv("TEMPER_OUT")
	}
}

func TestSuiteIgnoresInheritedOut(t *testing.T) {
	temper.Covers(t, "cli.out")
	if v := os.Getenv("TEMPER_OUT"); v != "" {
		t.Fatalf("TEMPER_OUT=%q leaked into the tests; fixtures would write there", v)
	}
}
