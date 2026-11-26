"""Application traffic: what real applications send PostgreSQL, captured from
a server running with log_statement = all and log_line_prefix = ''.

    oracle weights   writes /corpus/weights.json from /corpus/apps/*.sql

Each application's statements are a corpus file (corpus/apps/<app>.sql), and
together they weight the inventory: a member's weight is how often the
captured statements use it, divided by the most-used member's count. A
member no application uses gets FLOOR.
"""
import json
import re
import sys
from collections import Counter
from pathlib import Path

FLOOR = 0.01
_ENTRY = re.compile(r"^(LOG|DETAIL|ERROR|STATEMENT|WARNING|NOTICE|HINT|CONTEXT|FATAL|PANIC|DEBUG\d?|INFO):  ")
_STMT = re.compile(r"^LOG:  (?:statement|execute [^:]*): (.*)$", re.S)
_PARAMS = re.compile(r"^DETAIL:  parameters: (.*)$", re.S | re.I)  # "Parameters:" from PostgreSQL 16
_PARAM = re.compile(r"\$(\d+) = (NULL|'(?:[^']|'')*')")


def _entries(log: str):
    """Log entries, with tab-indented continuation lines joined."""
    cur = None
    for line in log.splitlines():
        if _ENTRY.match(line):
            if cur is not None:
                yield cur
            cur = line
        elif cur is not None and line.startswith("\t"):
            cur += "\n" + line[1:]
        elif cur is not None:
            yield cur
            cur = None
    if cur is not None:
        yield cur


def _inline(sql: str, params: str) -> str:
    values = {int(n): v for n, v in _PARAM.findall(params)}
    return re.sub(r"\$(\d+)\b", lambda m: values.get(int(m.group(1)), m.group(0)), sql)


def from_log(log: str) -> list:
    """Statements, in order, with bind parameters inlined as the log quotes them."""
    out = []
    pending_param = False
    for e in _entries(log):
        m = _STMT.match(e)
        if m:
            out.append(m.group(1).strip())
            pending_param = e.startswith("LOG:  execute")
            continue
        p = _PARAMS.match(e)
        if p and pending_param and out:
            out[-1] = _inline(out[-1], p.group(1))
        pending_param = False
    return out


def distinct(statements: list) -> list:
    """The first statement of each shape (pglast fingerprint), in order.
    Statements pglast can't parse are dropped."""
    import pglast
    seen, out = set(), []
    for sql in statements:
        try:
            fp = pglast.parser.fingerprint(sql)
        except Exception:
            continue
        if fp not in seen:
            seen.add(fp)
            out.append(sql)
    return out


def ref_counts(statements: list, inventory: dict) -> dict:
    """How often the statements use each inventory member."""
    from compat.tag import tag
    counts = Counter()
    for sql in statements:
        try:
            counts.update(tag(sql, inventory))
        except Exception:
            continue  # a captured statement pglast can't parse adds nothing
    return dict(sorted(counts.items()))


def weights(app_files: dict, inventory: dict) -> dict:
    """ref -> weight in (0, 1], for every inventory member."""
    return weights_from_counts([ref_counts(s, inventory) for s in app_files.values()], inventory)


def weights_from_counts(per_app: list, inventory: dict) -> dict:
    counts = Counter()
    for c in per_app:
        counts.update(c)
    top = max(counts.values(), default=0)
    out = {}
    for lattice, keys in inventory.items():
        for k in keys:
            ref = f"{lattice}/{k}"
            out[ref] = max(counts[ref] / top, FLOOR) if top and counts[ref] else FLOOR
    return out


def capture_file(app: str, version: str, log_path: Path, corpus: Path) -> None:
    """Write corpus/apps/<app>.sql (each statement shape once, in order) and
    corpus/apps/<app>.refs.json (member counts over the full traffic)."""
    from compat.inventory import keys, load
    stmts = from_log(Path(log_path).read_text(errors="replace"))
    inv = keys(load(Path("/lattices")))
    out = corpus / "apps"
    out.mkdir(parents=True, exist_ok=True)
    body = [f"-- Captured from {app} {version} against PostgreSQL 17.12 with log_statement = all.",
            f"-- {len(stmts)} statements captured; each statement shape appears once, in first-seen order.", ""]
    body += [s.rstrip().rstrip(";") + ";" for s in distinct(stmts)]
    (out / f"{app}.sql").write_text("\n".join(body) + "\n")
    (out / f"{app}.refs.json").write_text(json.dumps(ref_counts(stmts, inv), indent=0) + "\n")
    print(f"{app}: {len(stmts)} statements, {len(body) - 3} distinct")


def main(argv) -> int:
    from compat.inventory import keys, load
    corpus = Path("/corpus")
    if argv and argv[0] == "file":   # oracle weights file APP VERSION LOG
        capture_file(argv[1], argv[2], Path(argv[3]), corpus)
        return 0
    inv = keys(load(Path("/lattices")))
    per_app = [json.loads(p.read_text()) for p in sorted((corpus / "apps").glob("*.refs.json"))]
    w = weights_from_counts(per_app, inv)
    files = per_app
    (corpus / "weights.json").write_text(json.dumps(dict(sorted(w.items())), indent=0) + "\n")
    used = len({ref for c in per_app for ref in c})
    print(f"weights: {used} of {len(w)} members used by {len(files)} applications")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
