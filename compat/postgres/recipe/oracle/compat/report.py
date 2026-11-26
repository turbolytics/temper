"""REPORT.md for one subject: what's broken first, then the score.

    oracle report SUBJECT OUT_DIR [LEDGER]

The headline score is the weighted share of inventory members that pass.
A member's weight is how often captured application traffic uses it
(corpus/weights.json); a member no application uses gets FLOOR. Refused,
differing, documented, undecided and unexercised members all count as not
passing.
"""
import json
import sys
from collections import Counter, defaultdict
from pathlib import Path

from compat.verdict import FAILING, decide, load_outcomes

FLOOR = 0.01
SCORED = ("sql.command", "sql.syntax", "sql.function", "sql.operator", "sql.type")
NOT_SCORED = {
    "sql.standard": "no statement maps to an SQL-standard feature id yet",
    "server.setting": "PostgreSQL's own code under pg_duckdb; not scored",
    "server.errcode": "PostgreSQL's own code under pg_duckdb; not scored",
    "catalog.table": "PostgreSQL's own code under pg_duckdb; not scored",
}
TOP = 20


def weight(weights: dict, ref: str) -> float:
    return weights.get(ref, FLOOR)


def score(v, weights: dict, all_refs: set) -> float:
    total = sum(weight(weights, r) for r in all_refs)
    if not total:
        return 0.0
    return sum(weight(weights, r) for r in all_refs if r in v.passed) / total


def _cell(s, n=120):
    s = "" if s is None else str(s)
    s = " ".join(s.split())
    s = s.replace("|", "\\|")
    return s if len(s) <= n else s[: n - 1] + "…"


def _answer(r):
    if not r:
        return "(none)"
    if r.get("sqlstate"):
        return f"ERROR {r['sqlstate']}: {r.get('error') or ''}"
    return json.dumps(r.get("rows"))


def _silent(o) -> bool:
    return (o["outcome"] == "differs" and not (o.get("a") or {}).get("sqlstate")
            and not (o.get("b") or {}).get("sqlstate"))


def render_report(subject: str, v, outcomes: list, weights: dict, all_refs: set) -> str:
    L = [f"# {subject}: PostgreSQL compatibility", ""]
    counts = Counter(o["outcome"] for o in outcomes)
    L += ["Statements replayed: " + ", ".join(f"{k} {n}" for k, n in sorted(counts.items())), ""]

    L += ["## Crashes", ""]
    crashes = [o for o in outcomes if o["outcome"] == "crash"]
    if not crashes:
        L += ["None.", ""]
    else:
        L += ["| Statement | Where | Error |", "|---|---|---|"]
        for o in crashes:
            L.append(f"| `{_cell(o['sql'])}` | {o['file']}:{o['line']} | {_cell(o.get('reason'))} |")
        L.append("")

    L += ["## Silent wrong answers", "",
          "Both answered, without an error, and the answers differ. These are the reportable bugs, after a 10/10 rerun.", ""]
    blamed = {}
    for ref, f in v.failed.items():
        if _silent(f["example"]):
            blamed[ref] = f
    silent_rows = sorted(blamed.items(), key=lambda kv: (-weight(weights, kv[0]), kv[0]))[:TOP]
    if not silent_rows:
        L += ["None.", ""]
    else:
        L += ["| Feature | Statement | PostgreSQL | Subject | Reproduce |", "|---|---|---|---|---|"]
        for ref, f in silent_rows:
            e = f["example"]
            L.append(f"| `{ref}` | `{_cell(e['sql'], 80)}` | `{_cell(_answer(e['a']), 60)}` | "
                     f"`{_cell(_answer(e['b']), 60)}` | `oracle repro {subject} '{ref}'` |")
        L.append("")
    unattributed = [o for o in outcomes if _silent(o)]
    L += [f"Statements with a silent wrong answer, attributed or not: {len(unattributed)}.", ""]

    L += ["## Score", ""]
    s = score(v, weights, all_refs)
    L += [f"**{s:.1%}** of PostgreSQL's features pass, weighted by how often applications use them.", ""]
    L += ["| Lattice | Members | Pass | Fail | Documented | Undecided | Not exercised |", "|---|---|---|---|---|---|---|"]
    for lat in SCORED:
        refs = {r for r in all_refs if r.startswith(lat + "/")}
        if not refs:
            continue
        L.append(f"| `{lat}` | {len(refs)} | {len(refs & v.passed)} | {len(refs & set(v.failed))} | "
                 f"{len(refs & set(v.documented))} | {len(refs & set(v.undecided))} | {len(refs & v.unexercised)} |")
    flat = len(all_refs & v.passed)
    L += ["", f"Unweighted: {flat} of {len(all_refs)} members pass.", ""]

    L += ["## Applications", "",
          "Each application's share of its own captured statements that match, of those the subject ran.", ""]
    per_app = defaultdict(lambda: [0, 0])
    for o in outcomes:
        if o["file"].startswith("app:") and o["outcome"] not in ("noise", "postgres_executed"):
            per_app[o["file"][4:]][1] += 1
            if o["outcome"] == "match":
                per_app[o["file"][4:]][0] += 1
    if not per_app:
        L += ["No application traffic captured yet.", ""]
    else:
        L += ["| Application | Match | Share |", "|---|---|---|"]
        for app, (m, n) in sorted(per_app.items()):
            L.append(f"| {app} | {m} of {n} | {m / n:.1%} |")
        L.append("")

    L += ["## Documented differences", ""]
    if not v.documented:
        L += ["None.", ""]
    else:
        L += ["| Feature | Rule |", "|---|---|"]
        L += [f"| `{r}` | {_cell(rule)} |" for r, rule in sorted(v.documented.items())]
        L.append("")

    L += ["## Refused and other failures", ""]
    other = {r: f for r, f in v.failed.items() if r not in blamed}
    if not other:
        L += ["None.", ""]
    else:
        L += ["| Feature | Outcome | Statement | Subject |", "|---|---|---|---|"]
        for r, f in sorted(other.items(), key=lambda kv: (-weight(weights, kv[0]), kv[0])):
            e = f["example"]
            L.append(f"| `{r}` | {e['outcome']} | `{_cell(e['sql'], 80)}` | `{_cell(_answer(e['b']), 80)}` |")
        L.append("")

    L += ["## Undecided", "", "A failing statement used several unproven features; none is blamed.", ""]
    L += [", ".join(f"`{r}`" for r in sorted(v.undecided)) or "None.", ""]
    L += ["## Not exercised", "", "No statement the subject ran proves or disproves these.", ""]
    L += [", ".join(f"`{r}`" for r in sorted(v.unexercised & all_refs)) or "None.", ""]
    L += ["## Not scored", ""]
    L += [f"- `{lat}`: {why}." for lat, why in NOT_SCORED.items()]
    L += ["", "## Noise", "",
          f"{counts.get('noise', 0)} statements gave two different answers on PostgreSQL itself and were dropped.", ""]
    return "\n".join(L)


def main(argv) -> int:
    from compat.inventory import keys, load
    subject, out = argv[0], Path(argv[1])
    ledger = Path(argv[2] if len(argv) > 2 else "/ledger")
    inv = keys(load(Path("/lattices")))
    all_refs = {f"{lat}/{k}" for lat in SCORED for k in inv[lat]}
    outcomes = load_outcomes(ledger, subject, inv)
    v = decide(outcomes, all_refs)
    wpath = Path("/corpus/weights.json")
    weights = json.loads(wpath.read_text()) if wpath.exists() else {}
    out.mkdir(parents=True, exist_ok=True)
    (out / "REPORT.md").write_text(render_report(subject, v, outcomes, weights, all_refs))
    print(f"wrote {out / 'REPORT.md'}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
