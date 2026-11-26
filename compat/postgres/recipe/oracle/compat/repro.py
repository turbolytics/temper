"""Reproduce one cell.

    oracle repro SUBJECT '<lattice>/<key>' [LEDGER]

Finds the statement the cell's verdict rests on (the blamed statement of a
failing, documented or undecided cell; for a passing cell, the first that
proved it), replays its file up to and including it on both sides from a
fresh database (for a regression file, every earlier regression file first,
as the suite runs), and prints the statement and both answers. Exits 1 when
the statement still fails, 0 when it matches, 2 when the cell has no
statement.
"""
import json
import sys
import tempfile
from pathlib import Path

from compat import replay
from compat.corpus import CorpusFile
from compat.verdict import decide, load_outcomes


def _pick(v, ref, outcomes):
    if ref in v.failed:
        return v.failed[ref]["example"]
    if ref in v.undecided:
        return v.undecided[ref][0]
    for o in outcomes:
        if ref in o["refs"] and (o["outcome"] == "documented" or o["outcome"] == "match"):
            return o
    return None


def _slice(files, target):
    """The files to replay: earlier shared files, then target's file up to the statement."""
    out = []
    for f in files:
        if f.name == target["file"]:
            cut = CorpusFile(f.name, f.source, shared_db=f.shared_db)
            for s in f.statements:
                cut.statements.append(s)
                if s.line == target["line"] and s.sql == target["sql"]:
                    break
            out.append(cut)
            return out
        if f.shared_db:
            out.append(f)
    return out


def _show(label, r):
    if r is None:
        return f"{label}: (no answer)"
    if r.get("sqlstate"):
        return f"{label}: ERROR {r['sqlstate']} {r.get('error')}"
    return f"{label}: {json.dumps(r.get('cols'))} {json.dumps(r.get('rows'))}"


def repro(subject: str, ref: str, ledger: Path, files: list | None = None, inventory: dict | None = None) -> int:
    from compat.corpus import load
    outcomes = load_outcomes(ledger, subject, inventory)
    v = decide(outcomes)
    target = _pick(v, ref, outcomes)
    if target is None:
        print(f"{ref}: no statement in the corpus decides this cell for {subject}")
        return 2
    files = files if files is not None else load(Path("/pg"), Path("/corpus"))
    with tempfile.TemporaryDirectory() as tmp:
        replay.run(subject, _slice(files, target), Path(tmp), inventory=inventory)
        last = json.loads((Path(tmp) / subject / "outcomes.jsonl").read_text().splitlines()[-1])
    print(f"cell: {ref} on {subject}")
    print(f"statement: {target['file']}:{target['line']}")
    print(f"  {last['sql']}")
    print(_show("PostgreSQL", last.get("a")))
    print(_show(subject, last.get("b")))
    print(f"outcome: {last['outcome']} {last.get('reason', '')}".rstrip())
    return 0 if last["outcome"] == "match" else 1


def main(argv) -> int:
    ledger = Path(argv[2] if len(argv) > 2 else "/ledger")
    return repro(argv[0], argv[1], ledger)


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
