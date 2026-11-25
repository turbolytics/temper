# Temper proves itself with Temper. `make check-matrix` is what CI runs.

TEMPER  := go run ./cmd/temper
EXAMPLE := examples/inventory

.PHONY: test evidence matrix check-matrix example example-matrix compat-postgres check-compat-postgres site serve-site refresh-projects og-image

test:
	go vet ./...
	go test ./...

# Fresh evidence for every level of this repository.
evidence:
	$(TEMPER) clean
	$(TEMPER) run --level unit -- go test ./...
	$(TEMPER) run --level integration -- go test -tags integration -run '^TestIntegration' ./...

# Regenerate temper/status and MATRIX.md from fresh evidence.
matrix: evidence
	$(TEMPER) matrix --all-levels

# Fail if the committed statuses or MATRIX.md disagree with fresh evidence.
check-matrix: evidence
	$(TEMPER) matrix --check --all-levels

# The example, end to end: fresh evidence, then a check of its committed matrix.
example:
	$(TEMPER) --root $(EXAMPLE) clean
	$(TEMPER) --root $(EXAMPLE) run --level unit -- go -C $(EXAMPLE) test ./...
	$(TEMPER) --root $(EXAMPLE) run --level integration -- go -C $(EXAMPLE) test -tags integration -run '^TestIntegration' ./...
	$(TEMPER) --root $(EXAMPLE) matrix --check --all-levels

# Regenerate the example's committed matrix.
example-matrix:
	$(TEMPER) --root $(EXAMPLE) clean
	$(TEMPER) --root $(EXAMPLE) run --level unit -- go -C $(EXAMPLE) test ./...
	$(TEMPER) --root $(EXAMPLE) run --level integration -- go -C $(EXAMPLE) test -tags integration -run '^TestIntegration' ./...
	$(TEMPER) --root $(EXAMPLE) matrix --all-levels

# The Postgres compatibility matrix. compat/postgres holds the method only;
# its results go to a checkout of the private results repository:
#   make compat-postgres RESULTS=../compat-results
COMPAT        := compat/postgres
COMPAT_SUBJECTS := postgres.reference pg_duckdb.release

compat-postgres:
	@test -n "$(RESULTS)" || { echo "set RESULTS to a checkout of the private results repository"; exit 2; }
	$(TEMPER) --root $(COMPAT) clean --out $(abspath $(RESULTS))/postgres
	for s in $(COMPAT_SUBJECTS); do \
		$(TEMPER) --root $(COMPAT) run --out $(abspath $(RESULTS))/postgres --level compat -- \
			$(TEMPER) --root $(COMPAT) scenarios run --scenario compat.replay --subject $$s --build || true; \
	done
	$(TEMPER) --root $(COMPAT) matrix --all-levels --out $(abspath $(RESULTS))/postgres || test $$? -eq 1
	for s in $(COMPAT_SUBJECTS); do \
		docker compose -f $(COMPAT)/compose.yml run --rm -v $(abspath $(RESULTS))/postgres:/results oracle report $$s /results/reports/$$s; \
	done

# Without RESULTS, only the declarations are checked (CI has no results).
check-compat-postgres:
	$(TEMPER) --root $(COMPAT) check
	@if [ -n "$(RESULTS)" ]; then $(TEMPER) --root $(COMPAT) matrix --check --all-levels --out $(abspath $(RESULTS))/postgres; fi

# The website, for a local preview. Analytics is off.
site:
	go run ./site -out site/_build -no-analytics

# Build the website, then serve it at http://localhost:8000.
serve-site: site
	python3 -m http.server 8000 --directory site/_build

# Update the GitHub stars in findings/projects.csv. Set GITHUB_TOKEN to raise
# the API's rate limit. Commit the result.
refresh-projects:
	go run ./site -refresh-projects

# Re-render the link-preview image from its source. Needs rsvg-convert
# (brew install librsvg). Commit both files.
og-image:
	rsvg-convert -w 1200 -h 630 site/og.svg -o site/og.png
