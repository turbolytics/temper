package main

import (
	"strings"
	"testing"

	"github.com/turbolytics/temper"
	"github.com/turbolytics/temper/internal/recipes"
)

func TestRecipes_List(t *testing.T) {
	temper.Covers(t, "cli.recipes")
	code, out, errOut := cli(t, "recipes", "list")
	if code != 0 || errOut != "" {
		t.Fatalf("recipes list = %d, %q", code, errOut)
	}
	for _, p := range recipes.List() {
		if !strings.Contains(out, p.Pack) || !strings.Contains(out, strings.Fields(p.Doc)[0]) {
			t.Errorf("list lacks %s or its doc:\n%s", p.Pack, out)
		}
	}
	for _, want := range []string{"postgres", "service", "http", "temper recipes show"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q", want)
		}
	}
}

func TestRecipes_Show(t *testing.T) {
	temper.Covers(t, "cli.recipes")
	code, out, _ := cli(t, "recipes", "show", "postgres")
	pg, _ := recipes.Get("postgres")
	if code != 0 || out != string(pg.Source) {
		t.Fatalf("recipes show postgres = %d; did not print the pack verbatim:\n%.300s", code, out)
	}
}

func TestRecipes_Usage(t *testing.T) {
	temper.Covers(t, "cli.recipes")
	for _, args := range [][]string{{"recipes"}, {"recipes", "show"}, {"recipes", "show", "nope"}, {"recipes", "frobnicate"}} {
		if code, _, errOut := cli(t, args...); code != 2 || errOut == "" {
			t.Errorf("%v = %d, stderr %q; want 2 with a message", args, code, errOut)
		}
	}
	if _, _, errOut := cli(t, "recipes", "show", "nope"); !strings.Contains(errOut, "nope") {
		t.Errorf("unknown pack message = %q", errOut)
	}
}
