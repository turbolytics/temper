"""From statement outcomes to a verdict per inventory member.

A member passes when a statement using it matched and PostgreSQL itself
accepted that statement. A failing statement (differs, refused, crash)
blames the members in it that nothing has proven: exactly one, and that
member fails with this statement as its reproduction; more than one, and
each is undecided, never guessed. A documented difference blames the same
way into its own column.

    oracle check SUBJECT LATTICE [LEDGER]   prints {"<lattice>/<key>": violations}
    oracle check SUBJECT compat [LEDGER]    prints {"compat.matches_postgres": n}
"""
import json
import sys
from dataclasses import dataclass, field
from pathlib import Path

FAILING = ("differs", "refused", "crash")


@dataclass
class Verdicts:
    passed: set = field(default_factory=set)
    failed: dict = field(default_factory=dict)       # ref -> {"count", "example"}
    undecided: dict = field(default_factory=dict)    # ref -> [statements]
    unexercised: set = field(default_factory=set)
    documented: dict = field(default_factory=dict)   # ref -> rule id


def _proves(o) -> bool:
    return o["outcome"] == "match" and (o.get("a") or {}).get("sqlstate") is None


def decide(outcomes: list, all_refs: set | None = None) -> Verdicts:
    v = Verdicts()
    for o in outcomes:
        if _proves(o):
            v.passed.update(o["refs"])
    seen = set()
    documented_by = {}
    for o in outcomes:
        refs = set(o["refs"])
        seen |= refs
        unproven = refs - v.passed
        if o["outcome"] in FAILING:
            if len(unproven) == 1:
                ref = next(iter(unproven))
                f = v.failed.setdefault(ref, {"count": 0, "example": o})
                f["count"] += 1
            else:
                for ref in unproven:
                    v.undecided.setdefault(ref, []).append(o)
        elif o["outcome"] == "documented" and len(unproven) == 1:
            documented_by.setdefault(next(iter(unproven)), o.get("rule", ""))
    for ref in v.failed:
        v.undecided.pop(ref, None)
    for ref, rule in documented_by.items():
        if ref not in v.failed:
            v.documented[ref] = rule
            v.undecided.pop(ref, None)
    decided = v.passed | set(v.failed) | set(v.undecided) | set(v.documented)
    v.unexercised = (seen | (all_refs or set())) - decided
    return v


def check_json(v: Verdicts, lattice: str) -> dict:
    """One lattice's row for a Temper results check: 0 for a passing member,
    the number of blamed statements for a failing or documented one. Undecided
    and unexercised members are left out, so Temper records them as errors."""
    prefix = lattice + "/"
    out = {}
    for ref in sorted(v.passed):
        if ref.startswith(prefix):
            out[ref] = 0
    for ref, f in v.failed.items():
        if ref.startswith(prefix):
            out[ref] = f["count"]
    for ref in v.documented:
        if ref.startswith(prefix):
            out[ref] = 1
    return dict(sorted(out.items()))


def load_outcomes(ledger: Path, subject: str, inventory: dict | None = None) -> list:
    """The replay's outcomes, each with its refs (tagged here if replay didn't)."""
    from compat.tag import tag
    out = []
    for line in (Path(ledger) / subject / "outcomes.jsonl").read_text().splitlines():
        o = json.loads(line)
        if "refs" not in o:
            o["refs"] = sorted(tag(o["sql"], inventory or {}))
        out.append(o)
    return out


def main(argv) -> int:
    from compat.inventory import keys, load
    subject, lattice = argv[0], argv[1]
    ledger = Path(argv[2] if len(argv) > 2 else "/ledger")
    inv = keys(load(Path("/lattices")))
    outcomes = load_outcomes(ledger, subject, inv)
    if lattice == "compat":
        bad = sum(1 for o in outcomes if o["outcome"] in FAILING + ("documented",))
        print(json.dumps({"compat.matches_postgres": bad}))
        return 0
    all_refs = {f"{lattice}/{k}" for k in inv[lattice]}
    v = decide(outcomes, all_refs)
    print(json.dumps(check_json(v, lattice)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
