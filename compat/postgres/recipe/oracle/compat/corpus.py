"""The statement corpus: PostgreSQL's regression tests, the examples in its
docs, and captured application traffic, as statements to replay.

psql meta-commands are never sent to a server; they are skipped and counted.
COPY ... FROM stdin keeps its inline data, and the regression tests' COPY
from a data file under abs_srcdir keeps that file, so the data loads the same
on every side.
"""
import html
import re
from dataclasses import dataclass, field
from pathlib import Path

import pglast

REGRESS = "src/test/regress"
DOC_FILES = ("func.sgml", "queries.sgml", "dml.sgml", "ddl.sgml", "datatype.sgml",
             "json.sgml", "textsearch.sgml", "rangetypes.sgml", "arrays.sgml", "rowtypes.sgml")


@dataclass
class Stmt:
    file: str
    line: int
    sql: str
    # For COPY ... FROM STDIN: the rows to send, inline, or a file under /pg.
    copy_data: str | None = None
    copy_file: str | None = None


@dataclass
class CorpusFile:
    name: str
    source: str
    statements: list = field(default_factory=list)
    skipped_meta: int = 0
    skipped_unparsed: int = 0
    # Regression files share one database, in schedule order, as pg_regress
    # runs them; doc and app files each get a fresh one.
    shared_db: bool = False


_META_LINE = re.compile(r"^\s*\\")
_META_TAIL = re.compile(r"\\(g\w*|crosstabview|watch)\b.*$")
_SET_FILE = re.compile(r"^\s*\\set\s+(\w+)\s+:abs_srcdir\s+'([^']+)'")
_COPY_VAR = re.compile(r"^\s*COPY\s+(.+?)\s+FROM\s+:'(\w+)'(.*?);?\s*$", re.I | re.S)
_FROM_STDIN = re.compile(r"\bFROM\s+stdin\b", re.I)


def _parses(sql: str) -> bool:
    try:
        pglast.parse_sql(sql)
        return True
    except Exception:
        return False


class _Lexer:
    """Finds top-level semicolons across lines: outside quotes, dollar
    quotes, quoted identifiers and comments, as psql does."""

    _DOLLAR = re.compile(r"\$([A-Za-z_][A-Za-z0-9_]*)?\$")

    def __init__(self):
        self.quote = None      # "'", '"', a dollar tag like "$body$", or "/*"
        self.depth = 0         # nested /* */ comments

    def cuts(self, line: str) -> list:
        """Offsets of statement-ending semicolons in line."""
        out, i, n = [], 0, len(line)
        while i < n:
            c = line[i]
            q = self.quote
            if q == "/*":
                if line.startswith("*/", i):
                    self.depth -= 1
                    i += 2
                    if self.depth == 0:
                        self.quote = None
                    continue
                if line.startswith("/*", i):
                    self.depth += 1
                    i += 2
                    continue
                i += 1
            elif q in ("'", '"'):
                if c == q:
                    if line.startswith(q * 2, i):
                        i += 2
                        continue
                    self.quote = None
                i += 1
            elif q:  # a dollar tag
                if line.startswith(q, i):
                    self.quote = None
                    i += len(q)
                    continue
                i += 1
            else:
                if line.startswith("--", i):
                    break
                if line.startswith("/*", i):
                    self.quote, self.depth = "/*", 1
                    i += 2
                    continue
                if c in ("'", '"'):
                    self.quote = c
                    i += 1
                    continue
                if c == "$":
                    m = self._DOLLAR.match(line, i)
                    if m and not (i > 0 and (line[i - 1].isalnum() or line[i - 1] == "_")):
                        self.quote = m.group(0)
                        i = m.end()
                        continue
                if c == ";":
                    out.append(i)
                i += 1
        return out


def parse_file(name: str, text: str, source: str = "", shared_db: bool = False,
               regress_dir: str = REGRESS) -> CorpusFile:
    """Split a psql script into statements, skipping meta-commands."""
    f = CorpusFile(name, source or name, shared_db=shared_db)
    files = {}           # psql variables set to a data file under abs_srcdir
    lex = _Lexer()
    buf, start = "", None
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        line = lines[i]
        i += 1
        if lex.quote is None and not buf.strip():
            m = _SET_FILE.match(line)
            if m:
                files[m.group(1)] = regress_dir + m.group(2)
            if _META_LINE.match(line):
                f.skipped_meta += 1
                continue
        if lex.quote is None and _META_TAIL.search(line):
            f.skipped_meta += 1
            line = _META_TAIL.sub(";", line)
        if start is None and line.strip():
            start = i
        prev = 0
        for cut in lex.cuts(line):
            buf += line[prev:cut]
            prev = cut + 1
            sql = buf.strip()
            buf = ""
            if sql:
                i = _emit(f, sql, start, files, lines, i)
            start = None
        rest = line[prev:]
        buf += rest + "\n"
        if start is None and rest.strip():
            start = i
    if buf.strip():
        _emit(f, buf.strip(), start or len(lines), files, lines, len(lines))
    return f


def _emit(f: CorpusFile, sql: str, line: int, files: dict, lines: list, i: int) -> int:
    """Add one statement; return the next line index (past any COPY data)."""
    stmt = Stmt(f.name, line, sql)
    cv = _COPY_VAR.match(sql)
    if cv and cv.group(2) in files:
        stmt.sql = f"COPY {cv.group(1)} FROM STDIN{cv.group(3)}".rstrip()
        stmt.copy_file = files[cv.group(2)]
    elif sql.lstrip().upper().startswith("COPY") and _FROM_STDIN.search(sql):
        data = []
        while i < len(lines) and lines[i] != "\\.":
            data.append(lines[i])
            i += 1
        i += 1  # the \. terminator
        stmt.copy_data = "\n".join(data) + ("\n" if data else "")
    if _parses(stmt.sql):
        f.statements.append(stmt)
    else:
        f.skipped_unparsed += 1
    return i


def _split(chunk: str) -> list:
    """Statements in one complete chunk of SQL (no meta-commands)."""
    lex, out, buf = _Lexer(), [], ""
    for line in chunk.splitlines():
        prev = 0
        for cut in lex.cuts(line):
            buf += line[prev:cut]
            prev = cut + 1
            if buf.strip():
                out.append(buf.strip())
            buf = ""
        buf += line[prev:] + "\n"
    if buf.strip():
        out.append(buf.strip())
    return out


def regression(pg: Path) -> list:
    out = []
    regress = pg / REGRESS
    for line in (regress / "parallel_schedule").read_text().splitlines():
        if not line.startswith("test:"):
            continue
        for test in line.split(":", 1)[1].split():
            path = regress / "sql" / f"{test}.sql"
            if path.exists():
                out.append(parse_file(f"regress:{test}", path.read_text(errors="replace"),
                                      f"{REGRESS}/sql/{test}.sql", shared_db=True, regress_dir=REGRESS))
    return out


_PROGRAM = re.compile(r"<programlisting>(.*?)</programlisting>", re.S)
_FUNC_EXAMPLE = re.compile(r"<literal>([^<]*(?:<[^>]+>[^<]*)*?)</literal>\s*<returnvalue>", re.S)
_TAG = re.compile(r"<[^>]+>")


def _unsgml(s: str) -> str:
    return html.unescape(_TAG.sub("", s))


def docs(pg: Path) -> list:
    out = []
    sgml = pg / "doc/src/sgml"
    for name in DOC_FILES:
        path = sgml / name
        if not path.exists():
            continue
        body = path.read_text(errors="replace")
        f = CorpusFile(f"doc:{name}", f"doc/src/sgml/{name}")
        for m in _PROGRAM.finditer(body):
            line = body.count("\n", 0, m.start()) + 1
            for sql in _split(_unsgml(m.group(1))):
                if _parses(sql):
                    f.statements.append(Stmt(f.name, line, sql))
                else:
                    f.skipped_unparsed += 1
        # func.sgml's tables: an example expression and the value it returns.
        for m in _FUNC_EXAMPLE.finditer(body):
            expr = _unsgml(m.group(1)).strip()
            sql = f"SELECT {expr}"
            line = body.count("\n", 0, m.start()) + 1
            if _parses(sql):
                f.statements.append(Stmt(f.name, line, sql))
            else:
                f.skipped_unparsed += 1
        out.append(f)
    return out


def apps(corpus_dir: Path) -> list:
    out = []
    for path in sorted((corpus_dir / "apps").glob("*.sql")):
        out.append(parse_file(f"app:{path.stem}", path.read_text(errors="replace"), f"corpus/apps/{path.name}"))
    return out


def load(pg: Path, corpus_dir: Path | None = None) -> list:
    files = regression(pg) + docs(pg)
    if corpus_dir is not None and (corpus_dir / "apps").exists():
        files += apps(corpus_dir)
    return files
