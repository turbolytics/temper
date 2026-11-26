"""What a product claims (subjects/<name>/scope.yml), and so what its score counts.

    claim: {cite: "<where the product's docs say what it does>"}
    not_claimed:
      - members: ["sql.command/*", "sql.syntax/RETURNING"]
        why: "<one line>"
        cite: "<the product's docs, page and words>"

We set the scope from the product's public docs, never from the vendor's
say-so, so a product cannot raise its score by claiming less. Every entry
cites the docs, and a vendor can dispute one during the review. The harness
also detects, statement by statement, when PostgreSQL ran a statement in the
product's place; such a statement proves nothing either way.
"""
from pathlib import Path

import yaml

from compat.documented import ROOT, SUBJECT_DIRS


def load(subject: str, root: Path | None = None) -> list:
    """The product's not-claimed entries; [] for a product that claims everything."""
    d = SUBJECT_DIRS.get(subject)
    if d is None:
        return []
    path = (root or ROOT) / d / "scope.yml"
    if not path.exists():
        return []
    doc = yaml.safe_load(path.read_text()) or {}
    entries = doc.get("not_claimed") or []
    for e in entries:
        if not e.get("cite") or not e.get("why") or not e.get("members"):
            raise ValueError(f"{path}: every not_claimed entry needs members, why and cite: {e}")
    return entries


def not_claimed(subject: str, inventory: dict, root: Path | None = None) -> set:
    """The inventory members the product does not claim."""
    out = set()
    for e in load(subject, root):
        for m in e["members"]:
            lattice, _, key = m.partition("/")
            if key == "*":
                out |= {f"{lattice}/{k}" for k in inventory.get(lattice, ())}
            elif key in inventory.get(lattice, ()):
                out.add(m)
    return out
