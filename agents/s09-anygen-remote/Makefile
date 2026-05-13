.PHONY: build test demo clean

build:
	go build -o s09-anygen-remote .

test:
	go test ./...

# demo runs an in-process httptest.Server that imitates the AnyGen API
# and then runs the submit → poll → fetch cycle against it. No network,
# no API key needed. The script lives in demo/demo.go with a build tag
# that keeps it out of `go test ./...`.
demo:
	go run demo/demo.go

clean:
	rm -f s09-anygen-remote
