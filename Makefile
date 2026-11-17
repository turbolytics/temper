# Temper proves itself with Temper. `make check-matrix` is what CI runs.

TEMPER  := go run ./cmd/temper
EXAMPLE := examples/inventory

.PHONY: test evidence matrix check-matrix example example-matrix site serve-site refresh-projects

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
