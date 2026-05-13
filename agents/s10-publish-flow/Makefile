.PHONY: build test demo demo-json clean

build:
	go build -o s10-publish-flow .

test:
	go test ./...

# `demo` runs the publisher against testdata/ and prints the human view
# of the pipeline report. The out/ directory is wiped first so re-runs
# are reproducible.
demo: build
	rm -rf out
	./s10-publish-flow run testdata out
	@echo
	@echo "--- artifacts ---"
	@ls -la out

# Same but with --json so you see the Result envelope CI would log.
demo-json: build
	rm -rf out
	./s10-publish-flow --json run testdata out

# Status reads the registry.json from a prior `make demo` run.
status: build
	./s10-publish-flow status out

clean:
	rm -f s10-publish-flow
	rm -rf out
