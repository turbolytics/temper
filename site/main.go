// Command site builds the Temper by Turbolytics website from findings.csv
// and MATRIX.md. Run it from the repository root:
//
//	go run ./site -out site/_build
//	go run ./site -out site/_build -no-analytics   // local preview
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	out := flag.String("out", "site/_build", "directory to write the site to")
	root := flag.String("root", ".", "repository root")
	noAnalytics := flag.Bool("no-analytics", false, "leave out the PostHog snippet")
	flag.Parse()
	if err := Build(Options{Root: *root, Out: *out, Analytics: !*noAnalytics}); err != nil {
		fmt.Fprintf(os.Stderr, "site: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote the site to %s\n", *out)
}
