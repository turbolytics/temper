package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/turbolytics/temper/internal/evidence"
)

// cmdRun runs a command with the level and evidence directory set, so a test
// never chooses its own level (R4). It never deletes evidence: a level may be
// built from several commands, each wrapped in its own `temper run`.
func cmdRun(root string, args []string, stdout, stderr io.Writer) int {
	fs, rootFlag := flags("run", root, stderr)
	level := fs.String("level", "", "the level this command runs at (required)")
	runID := fs.String("run", "", "run id (default: $TEMPER_RUN, else the current UTC time)")
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	command := fs.Args()
	if *level == "" || len(command) == 0 {
		fmt.Fprint(stderr, "usage: temper run --level LEVEL [--run ID] -- COMMAND [ARGS...]\n")
		return exitError
	}

	// Declaration errors are matrix's to report; a run needs only the config.
	p, errs := project(*rootFlag)
	if p == nil || len(p.Config.Levels) == 0 {
		printErrors(stderr, errs)
		return exitError
	}
	if !slices.Contains(p.Config.LevelNames(), *level) {
		fmt.Fprintf(stderr, "%s: level %q is not declared (§14.9)\n", p.ConfigFile(), *level)
		return exitError
	}
	dir := p.EvidenceDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(stderr, "temper: %v\n", err)
		return exitError
	}
	id := *runID
	if id == "" {
		id = os.Getenv("TEMPER_RUN")
	}
	if id == "" {
		id = time.Now().UTC().Format("20060102T150405Z")
	}

	cmd := exec.Command(command[0], command[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	cmd.Env = append(withoutTemper(os.Environ()),
		"TEMPER_LEVEL="+*level, "TEMPER_EVIDENCE="+dir, "TEMPER_RUN="+id)
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		fmt.Fprintf(stderr, "temper: %v\n", err)
		return exitError
	}
	return exitOK
}

func withoutTemper(env []string) []string {
	var out []string
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TEMPER_LEVEL=") && !strings.HasPrefix(kv, "TEMPER_EVIDENCE=") && !strings.HasPrefix(kv, "TEMPER_RUN=") {
			out = append(out, kv)
		}
	}
	return out
}

// cmdClean deletes all evidence: the start of a full local run.
func cmdClean(root string, args []string, stdout, stderr io.Writer) int {
	fs, rootFlag := flags("clean", root, stderr)
	if err := fs.Parse(args); err != nil {
		return exitError
	}
	p, errs := project(*rootFlag)
	if p == nil || len(p.Config.Levels) == 0 && len(errs) != 0 {
		printErrors(stderr, errs)
		return exitError
	}
	n, err := evidence.Clean(p)
	if err != nil {
		fmt.Fprintf(stderr, "temper: %v\n", err)
		return exitError
	}
	fmt.Fprintf(stdout, "removed %d evidence files\n", n)
	return exitOK
}
