.PHONY: build test demo demo-json clean

build:
	go build -o s01-min-harness .

test:
	go test ./...

demo: build
	./s01-min-harness echo hello world
	./s01-min-harness time

demo-json: build
	./s01-min-harness --json echo hi
	./s01-min-harness --json
	./s01-min-harness --json time --format unix

clean:
	rm -f s01-min-harness
