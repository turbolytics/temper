package recipes

import (
	"reflect"
	"strings"
	"testing"

	"github.com/turbolytics/temper"
)

// required gives each pack's required parameters a value.
var required = map[string]map[string]string{
	"service": {"target": "worker-*"},
	"http":    {"from": "client", "base": "http://api:8080"},
}

func TestPacks_AllLoadAndExpand(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	packs := List()
	var names []string
	for _, p := range packs {
		names = append(names, p.Pack)
		if p.Doc == "" || len(p.Source) == 0 {
			t.Errorf("%s has no doc or source", p.Pack)
		}
		cmds, faults, err := Expand(p, "", required[p.Pack])
		if err != nil {
			t.Errorf("%s: %v", p.Pack, err)
			continue
		}
		for n, c := range cmds {
			forms := 0
			for _, f := range [][]string{c.Exec, c.Run, c.Host} {
				if len(f) > 0 {
					forms++
				}
			}
			if forms != 1 || c.Doc == "" {
				t.Errorf("%s: %d forms, doc %q", n, forms, c.Doc)
			}
			if !strings.HasPrefix(n, p.Pack+".") {
				t.Errorf("%s is not prefixed with %s.", n, p.Pack)
			}
		}
		for n := range faults {
			if !strings.HasPrefix(n, p.Pack+".") {
				t.Errorf("%s is not prefixed with %s.", n, p.Pack)
			}
		}
	}
	if !reflect.DeepEqual(names, []string{"http", "postgres", "service"}) {
		t.Errorf("packs = %v", names)
	}
}

func TestExpand_PrefixesAndFills(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	pg, _ := Get("postgres")
	cmds, faults, err := Expand(pg, "pg", map[string]string{"db": "river"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cmds["pg.sql"].Exec; !reflect.DeepEqual(got, []string{"postgres", "psql", "-U", "postgres", "-d", "river", "-Atc"}) {
		t.Errorf("pg.sql = %v", got)
	}
	if f := faults["pg.crash"]; f.Target != "postgres" || f.Do != "kill" || f.Then != "start" {
		t.Errorf("pg.crash = %+v", f)
	}
	if f := faults["pg.terminate"]; f.Do != "exec" || f.Exec.List[0] != "postgres" || !strings.Contains(f.Exec.List[len(f.Exec.List)-1], "LIKE '%'") {
		t.Errorf("pg.terminate = %+v", f)
	}
	svc, _ := Get("service")
	_, faults, err = Expand(svc, "worker", map[string]string{"target": "worker-*"})
	if err != nil || faults["worker.pause"].Target != "worker-*" {
		t.Errorf("worker.pause = %+v, %v", faults["worker.pause"], err)
	}
}

func TestExpand_UnknownParamIsError(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	pg, _ := Get("postgres")
	if _, _, err := Expand(pg, "", map[string]string{"database": "river"}); err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("Expand = %v", err)
	}
}

func TestExpand_MissingRequiredIsError(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	svc, _ := Get("service")
	if _, _, err := Expand(svc, "", nil); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("Expand = %v", err)
	}
}

func TestExpand_UnknownPlaceholderIsError(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	p, err := parse([]byte("pack: typo\ndoc: d\nparams:\n  service: {default: pg}\ncommands:\n  sql: {doc: d, exec: [\"{servce}\", psql]}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Expand(p, "", nil); err == nil || !strings.Contains(err.Error(), "servce") || !strings.Contains(err.Error(), "typo") {
		t.Fatalf("Expand = %v", err)
	}
}

func TestExpand_KeepsUseArgs(t *testing.T) {
	temper.Covers(t, "declarations.recipe_packs")
	pg, _ := Get("postgres")
	cmds, _, err := Expand(pg, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	last := cmds["postgres.hold_lock"].Exec[len(cmds["postgres.hold_lock"].Exec)-1]
	if !strings.Contains(last, "{1}") || !strings.Contains(last, "{2}") {
		t.Errorf("hold_lock lost its use arguments: %q", last)
	}
}
