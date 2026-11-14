package main

import (
	"runtime/debug"
	"testing"

	"github.com/turbolytics/temper"
)

// A release build is stamped by GoReleaser (-X main.version=0.1.0). A
// `go install github.com/…/temper@v0.1.0` build is not, but Go records the
// module version in the binary, so version falls back to that. Both read the
// same, without a leading v.
func TestResolveVersion(t *testing.T) {
	temper.Covers(t, "cli.version")
	info := func(v string) *debug.BuildInfo { return &debug.BuildInfo{Main: debug.Module{Version: v}} }
	for _, tc := range []struct {
		name string
		set  string
		info *debug.BuildInfo
		want string
	}{
		{"stamped at release wins over build info", "0.1.0", info("v9.9.9"), "0.1.0"},
		{"go install of a tag", "dev", info("v0.1.0"), "0.1.0"},
		{"go install of a commit", "dev", info("v0.1.1-0.20261114190000-abcdef123456"), "0.1.1-0.20261114190000-abcdef123456"},
		{"a local build with uncommitted changes", "dev", info("v0.1.0+dirty"), "0.1.0+dirty"},
		{"go build outside a module version", "dev", info("(devel)"), "dev"},
		{"empty module version", "dev", info(""), "dev"},
		{"no build info at all", "dev", nil, "dev"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.set, tc.info); got != tc.want {
				t.Fatalf("resolveVersion(%q, %v) = %q, want %q", tc.set, tc.info != nil, got, tc.want)
			}
		})
	}
}
