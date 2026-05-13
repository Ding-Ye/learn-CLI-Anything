.PHONY: build test demo demo-json clean

build:
	go build -o s06-hub-registry .

test:
	go test ./...

# Demo uses the FileSource against testdata/registry.json so it runs
# offline. Pass --url to switch to HTTP.
demo: build
	@echo "==> hub list"
	./s06-hub-registry --file testdata/registry.json list
	@echo
	@echo "==> hub search blend"
	./s06-hub-registry --file testdata/registry.json search blend
	@echo
	@echo "==> hub info anygen"
	./s06-hub-registry --file testdata/registry.json info anygen

demo-json: build
	@echo "==> hub list --json"
	./s06-hub-registry --json --file testdata/registry.json list
	@echo
	@echo "==> hub info anygen --json"
	./s06-hub-registry --json --file testdata/registry.json info anygen

clean:
	rm -f s06-hub-registry
