"""Which inventory members a statement uses, read from PostgreSQL's own
parse tree (libpg_query, through pglast). Only members the inventory knows
are returned, so a user-defined function or an extension's is ignored.
"""
import re

import pglast
from pglast import ast
from pglast import enums as E
from pglast.visitors import Visitor

# Type names as the parser spells them -> format_type names, the inventory's keys.
TYPE_ALIASES = {
    "int2": "smallint", "int4": "integer", "int": "integer", "int8": "bigint",
    "float4": "real", "float8": "double precision", "bool": "boolean",
    "varchar": "character varying", "bpchar": "character", "char": "character",
    "decimal": "numeric", "timestamptz": "timestamp with time zone",
    "timestamp": "timestamp without time zone", "timetz": "time with time zone",
    "time": "time without time zone", "varbit": "bit varying",
}

_CREATE_NOISE = {"OR", "REPLACE", "TEMP", "TEMPORARY", "UNLOGGED", "UNIQUE", "GLOBAL", "LOCAL",
                 "RECURSIVE", "TRUSTED", "PROCEDURAL", "DEFAULT", "CONSTRAINT"}

_DML = {
    "InsertStmt": "INSERT", "UpdateStmt": "UPDATE", "DeleteStmt": "DELETE", "MergeStmt": "MERGE",
}


def _name(v) -> str:
    return getattr(v, "name", str(v))


def _strings(names) -> list:
    return [getattr(n, "sval", None) or str(n) for n in (names or ())]


def type_key(type_name) -> str:
    names = [n.lower() for n in _strings(type_name.names)]
    if names and names[0] == "pg_catalog":
        names = names[1:]
    base = names[-1] if names else ""
    return TYPE_ALIASES.get(base, base)


def _command(stmt, sql: str, commands: set) -> str | None:
    kind = type(stmt).__name__
    if kind == "SelectStmt":
        if stmt.intoClause is not None and "SELECT INTO" in commands:
            return "SELECT INTO"
        if stmt.valuesLists and "VALUES" in commands:
            return "VALUES"
        return "SELECT"
    if kind in _DML:
        return _DML[kind]
    words = [w.upper() for w in sql.replace("(", " ").split()[:8]]
    if words and words[0] == "CREATE":
        words = ["CREATE"] + [w for w in words[1:] if w not in _CREATE_NOISE]
    best = None
    for n in range(len(words), 0, -1):
        cand = " ".join(words[:n])
        if cand in commands:
            best = cand
            break
    return best


class _Tagger(Visitor):
    def __init__(self, inv: dict):
        super().__init__()
        self.inv = inv
        self.refs = set()

    def add(self, lattice: str, key: str):
        if key in self.inv.get(lattice, ()):
            self.refs.add(f"{lattice}/{key}")

    def syn(self, key: str):
        self.add("sql.syntax", key)

    def visit_JoinExpr(self, ancestors, node):
        jt = _name(node.jointype)
        if node.isNatural:
            self.syn("JOIN NATURAL")
        if node.usingClause:
            self.syn("JOIN USING")
        if jt == "JOIN_INNER":
            self.syn("JOIN CROSS" if node.quals is None and not node.isNatural and not node.usingClause else "JOIN INNER")
        else:
            self.syn({"JOIN_LEFT": "JOIN LEFT", "JOIN_RIGHT": "JOIN RIGHT", "JOIN_FULL": "JOIN FULL"}.get(jt, ""))

    def visit_RangeSubselect(self, ancestors, node):
        if node.lateral:
            self.syn("LATERAL")

    def visit_RangeFunction(self, ancestors, node):
        self.syn("set-returning function in FROM")
        if node.lateral:
            self.syn("LATERAL")
        if node.ordinality:
            self.syn("WITH ORDINALITY")
        if node.is_rowsfrom:
            self.syn("ROWS FROM")

    def visit_RangeTableSample(self, ancestors, node):
        self.syn("TABLESAMPLE")

    def visit_SelectStmt(self, ancestors, node):
        if node.distinctClause is not None:
            exprs = [d for d in node.distinctClause if d is not None]
            self.syn("DISTINCT ON" if exprs else "DISTINCT")
        if node.groupClause:
            self.syn("GROUP BY")
        if node.havingClause is not None:
            self.syn("HAVING")
        if node.windowClause:
            self.syn("WINDOW clause")
        if node.valuesLists:
            self.syn("VALUES")
        if node.sortClause:
            self.syn("ORDER BY")
        if node.limitCount is not None:
            if _name(node.limitOption) == "LIMIT_OPTION_WITH_TIES":
                self.syn("FETCH WITH TIES")
            self.syn("LIMIT")
        if node.limitOffset is not None:
            self.syn("OFFSET")
        if node.lockingClause:
            self.syn("FOR UPDATE/SHARE")
        if node.intoClause is not None:
            self.syn("SELECT INTO")
        op = _name(node.op)
        if op != "SETOP_NONE":
            base = {"SETOP_UNION": "UNION", "SETOP_INTERSECT": "INTERSECT", "SETOP_EXCEPT": "EXCEPT"}[op]
            self.syn(base + (" ALL" if node.all else ""))
        self._with(node.withClause)

    def _with(self, w):
        if w is None:
            return
        self.syn("WITH")
        if w.recursive:
            self.syn("WITH RECURSIVE")

    def visit_CommonTableExpr(self, ancestors, node):
        m = _name(node.ctematerialized)
        if m == "CTEMaterializeAlways":
            self.syn("WITH MATERIALIZED")
        elif m == "CTEMaterializeNever":
            self.syn("WITH NOT MATERIALIZED")

    def visit_GroupingSet(self, ancestors, node):
        k = _name(node.kind)
        self.syn({"GROUPING_SET_ROLLUP": "ROLLUP", "GROUPING_SET_CUBE": "CUBE", "GROUPING_SET_SETS": "GROUPING SETS"}.get(k, ""))

    def visit_SortBy(self, ancestors, node):
        nulls = _name(node.sortby_nulls)
        if nulls == "SORTBY_NULLS_FIRST":
            self.syn("ORDER BY NULLS FIRST")
        elif nulls == "SORTBY_NULLS_LAST":
            self.syn("ORDER BY NULLS LAST")
        if _name(node.sortby_dir) == "SORTBY_USING":
            self.syn("ORDER BY USING")
            for op in _strings(node.useOp):
                self.add("sql.operator", op)

    def visit_FuncCall(self, ancestors, node):
        names = [n.lower() for n in _strings(node.funcname)]
        if names[:1] == ["pg_catalog"]:
            names = names[1:]
        if _name(node.funcformat) == "COERCE_SQL_SYNTAX":
            if names and names[-1] == "timezone":
                self.syn("AT TIME ZONE")
        elif names:
            self.add("sql.function", names[-1])
        if node.agg_filter is not None:
            self.syn("aggregate FILTER")
        if node.agg_distinct:
            self.syn("aggregate DISTINCT")
        if node.agg_within_group:
            self.syn("WITHIN GROUP")
        elif node.agg_order:
            self.syn("aggregate ORDER BY")
        if node.func_variadic:
            self.syn("VARIADIC")
        if node.over is not None:
            self.syn("window function")

    def visit_WindowDef(self, ancestors, node):
        fo = node.frameOptions or 0
        if fo & E.FRAMEOPTION_NONDEFAULT:
            if fo & E.FRAMEOPTION_ROWS:
                self.syn("WINDOW FRAME ROWS")
            elif fo & E.FRAMEOPTION_GROUPS:
                self.syn("WINDOW FRAME GROUPS")
            elif fo & E.FRAMEOPTION_RANGE:
                self.syn("WINDOW FRAME RANGE")
            if fo & (E.FRAMEOPTION_EXCLUDE_CURRENT_ROW | E.FRAMEOPTION_EXCLUDE_GROUP | E.FRAMEOPTION_EXCLUDE_TIES):
                self.syn("WINDOW EXCLUDE")

    def visit_NamedArgExpr(self, ancestors, node):
        self.syn("named argument")

    def visit_CaseExpr(self, ancestors, node):
        self.syn("CASE")

    def visit_CoalesceExpr(self, ancestors, node):
        self.syn("COALESCE")

    def visit_MinMaxExpr(self, ancestors, node):
        self.syn("GREATEST" if _name(node.op) == "IS_GREATEST" else "LEAST")

    def visit_SubLink(self, ancestors, node):
        t = _name(node.subLinkType)
        if t == "EXISTS_SUBLINK":
            self.syn("EXISTS subquery")
        elif t == "ANY_SUBLINK":
            self.syn("IN subquery" if not node.operName else "ANY subquery")
        elif t == "ALL_SUBLINK":
            self.syn("ALL subquery")
        elif t == "EXPR_SUBLINK":
            self.syn("scalar subquery")
        elif t == "ARRAY_SUBLINK":
            self.syn("ARRAY subquery")
        for op in _strings(node.operName):
            self.add("sql.operator", op)

    def visit_A_Expr(self, ancestors, node):
        k = _name(node.kind)
        ops = _strings(node.name)
        if k in ("AEXPR_OP", "AEXPR_OP_ANY", "AEXPR_OP_ALL"):
            for op in ops:
                self.add("sql.operator", op)
            if k != "AEXPR_OP":
                self.syn("= ANY (array)")
        elif k in ("AEXPR_DISTINCT", "AEXPR_NOT_DISTINCT"):
            self.syn("IS DISTINCT FROM")
        elif k == "AEXPR_NULLIF":
            self.syn("NULLIF")
        elif k == "AEXPR_IN":
            self.syn("IN list")
        elif k == "AEXPR_LIKE":
            self.syn("LIKE")
        elif k == "AEXPR_ILIKE":
            self.syn("ILIKE")
        elif k == "AEXPR_SIMILAR":
            self.syn("SIMILAR TO")
        elif k in ("AEXPR_BETWEEN", "AEXPR_NOT_BETWEEN"):
            self.syn("BETWEEN")
        elif k in ("AEXPR_BETWEEN_SYM", "AEXPR_NOT_BETWEEN_SYM"):
            self.syn("BETWEEN SYMMETRIC")

    def visit_BoolExpr(self, ancestors, node):
        self.syn("boolean AND/OR/NOT")

    def visit_NullTest(self, ancestors, node):
        self.syn("IS NULL")

    def visit_BooleanTest(self, ancestors, node):
        self.syn("IS TRUE/FALSE/UNKNOWN")

    def visit_TypeCast(self, ancestors, node):
        self.syn("typecast")

    def visit_TypeName(self, ancestors, node):
        self.add("sql.type", type_key(node))

    def visit_A_Indirection(self, ancestors, node):
        for ind in node.indirection or ():
            if isinstance(ind, ast.A_Indices):
                self.syn("array slice" if ind.is_slice else "array subscript")
            elif isinstance(ind, ast.String):
                self.syn("field selection")

    def visit_A_ArrayExpr(self, ancestors, node):
        self.syn("ARRAY constructor")

    def visit_RowExpr(self, ancestors, node):
        self.syn("ROW constructor")

    def visit_CollateClause(self, ancestors, node):
        self.syn("COLLATE")

    def visit_InsertStmt(self, ancestors, node):
        oc = node.onConflictClause
        if oc is not None:
            self.syn("ON CONFLICT DO UPDATE" if _name(oc.action) == "ONCONFLICT_UPDATE" else "ON CONFLICT DO NOTHING")
        if node.returningList:
            self.syn("RETURNING")
        self._with(node.withClause)

    def visit_UpdateStmt(self, ancestors, node):
        if node.returningList:
            self.syn("RETURNING")
        self._with(node.withClause)

    def visit_DeleteStmt(self, ancestors, node):
        if node.returningList:
            self.syn("RETURNING")
        self._with(node.withClause)

    def visit_MergeStmt(self, ancestors, node):
        for w in node.mergeWhenClauses or ():
            self.syn("MERGE WHEN MATCHED" if _name(w.matchKind) == "MERGE_WHEN_MATCHED" else "MERGE WHEN NOT MATCHED")
        if node.returningList:
            self.syn("MERGE RETURNING")
        self._with(node.withClause)

    def visit_SQLValueFunction(self, ancestors, node):
        self.syn("SQL value function")

    def visit_XmlExpr(self, ancestors, node):
        self.syn("XML expression")

    def visit_XmlSerialize(self, ancestors, node):
        self.syn("XML expression")

    def visit_JsonObjectConstructor(self, ancestors, node):
        self.syn("JSON constructor")

    def visit_JsonArrayConstructor(self, ancestors, node):
        self.syn("JSON constructor")

    def visit_JsonArrayQueryConstructor(self, ancestors, node):
        self.syn("JSON constructor")

    def visit_JsonObjectAgg(self, ancestors, node):
        self.syn("JSON constructor")

    def visit_JsonArrayAgg(self, ancestors, node):
        self.syn("JSON constructor")

    def visit_JsonFuncExpr(self, ancestors, node):
        self.syn("JSON query function")

    def visit_JsonTable(self, ancestors, node):
        self.syn("JSON_TABLE")

    def visit_JsonIsPredicate(self, ancestors, node):
        self.syn("IS JSON")

    def visit_ParamRef(self, ancestors, node):
        self.syn("positional parameter")


def _root(sql: str):
    stmts = pglast.parse_sql(sql)
    return stmts[0].stmt if stmts else None


def tag(sql: str, inventory: dict) -> set:
    """Refs "<lattice>/<key>" for every inventory member the statement uses."""
    stmt = _root(sql)
    if stmt is None:
        return set()
    t = _Tagger(inventory)
    t(stmt)
    # The parse tree spells LIMIT and FETCH FIRST the same; the text doesn't.
    if "sql.syntax/LIMIT" in t.refs and re.search(r"\bFETCH\s+(FIRST|NEXT)\b", sql, re.I):
        t.add("sql.syntax", "FETCH FIRST")
    cmd = _command(stmt, sql, inventory.get("sql.command", set()))
    if cmd:
        t.add("sql.command", cmd)
    return t.refs


def ordered(sql: str) -> bool:
    """The statement's result order is defined: a top-level ORDER BY."""
    stmt = _root(sql)
    return isinstance(stmt, ast.SelectStmt) and bool(stmt.sortClause)


def is_query(sql: str) -> bool:
    """A statement that only reads and returns rows (SELECT, VALUES, TABLE)."""
    stmt = _root(sql)
    return isinstance(stmt, ast.SelectStmt) and stmt.intoClause is None and not stmt.lockingClause
