// Command site builds the Temper by Turbolytics website from findings.csv
// and MATRIX.md. Run it from the repository root:
//
//	go run ./site -out site/_build
//	go run ./site -out site/_build -no-analytics   // local preview
//	go run ./site -refresh-projects                 // update stars in findings/projects.csv
package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	out := flag.String("out", "site/_build", "directory to write the site to")
	root := flag.String("root", ".", "repository root")
	noAnalytics := flag.Bool("no-analytics", false, "leave out the PostHog snippet")
	refresh := flag.Bool("refresh-projects", false, "update findings/projects.csv from the GitHub API, then exit")
	flag.Parse()
	if *refresh {
		today := time.Now().UTC().Format(time.DateOnly)
		client := &http.Client{Timeout: 30 * time.Second}
		if err := RefreshProjects(*root, "https://api.github.com", today, client); err != nil {
			fmt.Fprintf(os.Stderr, "site: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("updated findings/projects.csv")
		return
	}
	if err := Build(Options{Root: *root, Out: *out, Analytics: !*noAnalytics}); err != nil {
		fmt.Fprintf(os.Stderr, "site: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("wrote the site to %s\n", *out)
}
