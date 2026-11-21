package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/turbolytics/temper/internal/recipes"
)

// cmdRecipes lists the built-in recipe packs, or prints one.
func cmdRecipes(args []string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 1 && args[0] == "list":
		for _, p := range recipes.List() {
			fmt.Fprintf(stdout, "%-10s %s\n", p.Pack, strings.Join(strings.Fields(p.Doc), " "))
		}
		fmt.Fprintln(stdout, "\nInclude a pack under packs: in temper.yml. temper recipes show PACK prints what each name does.")
		return exitOK
	case len(args) == 2 && args[0] == "show":
		p, ok := recipes.Get(args[1])
		if !ok {
			fmt.Fprintf(stderr, "temper: no recipe pack %q; temper recipes list shows them\n", args[1])
			return exitError
		}
		_, _ = stdout.Write(p.Source)
		return exitOK
	}
	fmt.Fprintf(stderr, "temper: recipes needs list, or show PACK\n\n%s", usage)
	return exitError
}
