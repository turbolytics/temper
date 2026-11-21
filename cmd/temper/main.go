// Command temper checks a project's declarations and joins them with the
// evidence its tests wrote.
//
//	temper check                     validate declarations
//	temper matrix [--json] [--check] [--all-levels]
//	temper render                    rewrite MATRIX.md from the committed statuses
//	temper run --level L -- CMD...   run CMD with the level and evidence directory set
//	temper scenarios run             run runner: temper scenarios under faults
//	temper recipes list|show PACK    the built-in recipe packs
//	temper clean                     delete all evidence
//	temper guide                     print the guide for working in a Temper project
//	temper version
//
// Exit codes: 0 ok; 1 an enforced cell is missing or failing; 2 a
// declaration, evidence or usage error; 3 committed output is stale
// (matrix --check).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/turbolytics/temper/docs"
	"github.com/turbolytics/temper/internal/check"
	"github.com/turbolytics/temper/internal/diag"
	"github.com/turbolytics/temper/internal/load"
	"github.com/turbolytics/temper/internal/model"
)

// version is set at release with -ldflags "-X main.version=...". A build made
// by `go install …@v0.1.0` is not stamped, so main falls back to the module
// version Go records in the binary (see resolveVersion).
var version = "dev"

// resolveVersion returns the version to report. A version stamped at build
// time wins. Otherwise it is the module version Go recorded in the binary,
// without its leading v so it reads the same as a stamped release: 0.1.0, not
// v0.1.0. A binary with no real module version (a plain `go build` reports
// "(devel)") stays "dev".
func resolveVersion(set string, info *debug.BuildInfo) string {
	if set != "dev" || info == nil {
		return set
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return set
	}
	return strings.TrimPrefix(v, "v")
}

const (
	exitOK    = 0
	exitGap   = 1
	exitError = 2
	exitStale = 3
)

// product is the name the CLI prints. The command stays `temper`.
const product = "Temper by Turbolytics"

const usage = product + `

usage: temper [--root DIR] <command> [flags]

commands:
  check                              validate declarations
  matrix [--json] [--check] [--all-levels]
                                     join evidence; write temper/status and MATRIX.md
  render                             rewrite MATRIX.md from the committed statuses
  run --level LEVEL [--run ID] -- COMMAND [ARGS...]
                                     run COMMAND with TEMPER_LEVEL and TEMPER_EVIDENCE set
  scenarios run [--scenario ID] [--subject ID] [--seed N] [--compose FILE] [--build]
                                     run runner: temper scenarios against Docker Compose
  recipes list | recipes show PACK   list the built-in recipe packs, or print one
  clean                              delete all evidence
  guide                              print the guide for working in a Temper project
  version                            print the version

exit codes: 0 ok, 1 gap, 2 error, 3 stale output (matrix --check)
`

func main() {
	info, _ := debug.ReadBuildInfo()
	version = resolveVersion(version, info)
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	global := flag.NewFlagSet("temper", flag.ContinueOnError)
	global.SetOutput(io.Discard)
	root := global.String("root", "", "project root (default: nearest directory with temper.yml)")
	if err := global.Parse(args); err != nil {
		fmt.Fprintf(stderr, "temper: %v\n\n%s", err, usage)
		return exitError
	}
	rest := global.Args()
	if len(rest) == 0 {
		fmt.Fprint(stderr, usage)
		return exitError
	}
	cmd, cmdArgs := rest[0], rest[1:]
	switch cmd {
	case "check":
		return cmdCheck(*root, cmdArgs, stdout, stderr)
	case "matrix":
		return cmdMatrix(*root, cmdArgs, stdout, stderr)
	case "render":
		return cmdRender(*root, cmdArgs, stdout, stderr)
	case "run":
		return cmdRun(*root, cmdArgs, stdout, stderr)
	case "clean":
		return cmdClean(*root, cmdArgs, stdout, stderr)
	case "scenarios":
		return cmdScenarios(*root, cmdArgs, stdout, stderr)
	case "recipes":
		return cmdRecipes(cmdArgs, stdout, stderr)
	case "guide":
		if len(cmdArgs) != 0 {
			fmt.Fprintf(stderr, "temper: guide takes no arguments\n\n%s", usage)
			return exitError
		}
		fmt.Fprint(stdout, docs.Agents)
		return exitOK
	case "version":
		fmt.Fprintf(stdout, "%s %s\n", product, version)
		return exitOK
	default:
		fmt.Fprintf(stderr, "temper: unknown command %q\n\n%s", cmd, usage)
		return exitError
	}
}

// flags returns a flag set for a command that also accepts --root.
func flags(name, root string, stderr io.Writer) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("temper "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs, fs.String("root", root, "project root")
}

// project finds the root and loads the declarations.
func project(root string) (*model.Project, diag.List) {
	if root == "" {
		found, err := load.FindRoot(".")
		if err != nil {
			return nil, diag.List{{File: "temper.yml", Message: err.Error()}}
		}
		root = found
	}
	return load.Load(root)
}

// checked loads and checks the project; any finding is an error.
func checked(root string) (*model.Project, diag.List) {
	p, errs := project(root)
	if len(errs) != 0 {
		return p, errs
	}
	return p, check.Check(p)
}

func printErrors(w io.Writer, errs diag.List) {
	for _, e := range errs {
		fmt.Fprintln(w, e.String())
	}
}

func cmdCheck(root string, args []string, stdout, stderr io.Writer) int {
	fs, rootFlag := flags("check", root, stderr)
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	p, errs := checked(*rootFlag)
	if len(errs) != 0 {
		printErrors(stderr, errs)
		return exitError
	}
	fmt.Fprintf(stdout, "ok: %d features, %d invariants, %d lattices, %d subjects, %d scenarios\n",
		len(p.Features), len(p.Invariants), len(p.Lattices), len(p.Subjects), len(p.Scenarios))
	return exitOK
}

func joinLevels(l []string) string { return strings.Join(l, ", ") }
