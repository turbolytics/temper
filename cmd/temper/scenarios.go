package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/model"
	"github.com/turbolytics/temper/internal/scenarios"
)

// newEngine builds the engine for a Compose file; tests replace it.
var newEngine = func(file, project string) scenarios.Engine {
	return scenarios.NewCompose(file, project, time.Minute, scenarios.ExecRun)
}

// runContext is cancelled by Ctrl-C or SIGTERM; tests replace it.
var runContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

var unsafeName = regexp.MustCompile(`[^a-z0-9_-]+`)

// composeProject names the Compose project for a scenario, so its
// containers never collide with the project's own.
func composeProject(id string) string {
	return "temper-" + strings.Trim(unsafeName.ReplaceAllString(strings.ToLower(id), "-"), "-")
}

func cmdScenarios(root string, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "run" {
		fmt.Fprintf(stderr, "temper: scenarios needs a subcommand: run\n\n%s", usage)
		return exitError
	}
	fs, rootFlag, outFlag := flags("scenarios run", root, stderr)
	only := fs.String("scenario", "", "run only this scenario")
	subject := fs.String("subject", "", "run only this subject of an expanded scenario")
	seedFlag := fs.String("seed", "", "replay this seed")
	composeFlag := fs.String("compose", "", "Compose file, replacing each scenario's compose")
	build := fs.Bool("build", false, "build the images first, so the run uses this checkout's code")
	if err := fs.Parse(args[1:]); err != nil {
		return exitError
	}
	var seed *int64
	if *seedFlag != "" {
		n, err := strconv.ParseInt(*seedFlag, 10, 64)
		if err != nil {
			fmt.Fprintf(stderr, "temper: --seed %q is not an integer\n", *seedFlag)
			return exitError
		}
		seed = &n
	}
	p, errs := checked(*rootFlag, *outFlag)
	if len(errs) != 0 {
		printErrors(stderr, errs)
		return exitError
	}
	var picked []model.Scenario
	for _, sc := range p.Scenarios {
		if sc.Runner == "temper" && (*only == "" || sc.ID == *only) {
			picked = append(picked, sc)
		}
	}
	if *only != "" && len(picked) == 0 {
		fmt.Fprintf(stderr, "temper: no runner: temper scenario %q\n", *only)
		return exitError
	}
	if *subject != "" {
		var keep []model.Scenario
		for _, sc := range picked {
			if _, ok := sc.Subjects[*subject]; ok || sc.Subject == *subject {
				keep = append(keep, sc)
			}
		}
		if len(keep) == 0 {
			fmt.Fprintf(stderr, "temper: no runner: temper scenario runs on subject %q\n", *subject)
			return exitError
		}
		picked = keep
	}
	if len(picked) == 0 {
		fmt.Fprintln(stdout, "no runner: temper scenarios")
		return exitOK
	}

	level, evDir, runID := os.Getenv("TEMPER_LEVEL"), os.Getenv("TEMPER_EVIDENCE"), os.Getenv("TEMPER_RUN")
	record := level != "" && evDir != ""
	ctx, cancel := runContext()
	defer cancel()
	code := exitOK
	for _, sc := range picked {
		file := sc.Compose
		if *composeFlag != "" {
			file = *composeFlag
		}
		if !filepath.IsAbs(file) {
			file = filepath.Join(p.Root, file)
		}
		e := newEngine(file, composeProject(sc.ID))
		vs, err := scenarios.RunScenario(ctx, e, p, sc, scenarios.Options{
			Seed: seed, Subject: *subject, Log: stderr, Build: *build, LogDir: filepath.Join(p.Root, ".temper", "scenarios"),
		})
		if err != nil {
			fmt.Fprintf(stderr, "temper: %v\n", err)
			return exitError
		}
		for _, v := range vs {
			line := fmt.Sprintf("%-5s %s", v.Outcome, v.Test)
			if v.Detail != "" {
				line += ": " + v.Detail
			}
			fmt.Fprintln(stdout, line)
			if v.Outcome != temper.OutcomePass {
				code = exitGap
			}
		}
		if record {
			if err := scenarios.WriteEvidence(evDir, level, runID, time.Now(), vs); err != nil {
				fmt.Fprintf(stderr, "temper: writing evidence: %v\n", err)
				return exitError
			}
		}
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "temper: interrupted; healed the fault in flight and tore down the topology")
			return exitGap
		}
	}
	return code
}
