.PHONY: build test demo demo-json clean

CACHE := /tmp/learn-cli-anything-s04-demo

build:
	go build -o s04-preview-bundle .

test:
	go test ./...

# demo: run `echo hello` twice with the same cache dir. First run is a
# fresh exec (cache_hit=false); second run is replayed from disk
# (cache_hit=true).
demo: build
	rm -rf $(CACHE)
	@echo "--- first run (expect cache_hit=false) ---"
	./s04-preview-bundle --json -cache $(CACHE) run -- echo hello
	@echo "--- second run (expect cache_hit=true) ---"
	./s04-preview-bundle --json -cache $(CACHE) run -- echo hello
	@echo "--- cache dir contents ---"
	@ls $(CACHE)

demo-json: demo

clean:
	rm -f s04-preview-bundle
	rm -rf $(CACHE)
