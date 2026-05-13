.PHONY: build test demo demo-json clean

build:
	go build -o s08-verify-plugin .

test:
	go test ./...

# Runs the verifier against the happy-path fixture under testdata/. The
# harness stub there prints "ok":true so every check should pass.
demo: build
	./s08-verify-plugin verify ./testdata/plugin-good

demo-json: build
	./s08-verify-plugin verify ./testdata/plugin-good --json

clean:
	rm -f s08-verify-plugin
