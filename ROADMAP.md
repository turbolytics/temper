# Roadmap

What is planned, in order. Each item gets a design in
`docs/superpowers/specs/` before it is built.

## Next

- **`temper ingest`.** Produce evidence with no Temper dependency. Tests
  print a `TEMPER covers=…` line. `temper run --ingest` reads `go test -json`
  or JUnit XML and writes the same records an adapter writes. Design:
  [2026-11-16-temper-ingest-design.md](docs/superpowers/specs/2026-11-16-temper-ingest-design.md).

## Later

- **`temper init` and `temper baseline`.** Start a project from today's gaps
  without failing CI, then ratchet.
- **Matrix on GitHub Pages, and a badge.** A sample workflow publishes
  `temper render` output to the repo's own Pages.
- **Production evidence.** A level for what is observed at runtime, with
  cells marked invoked, dormant or violated.
