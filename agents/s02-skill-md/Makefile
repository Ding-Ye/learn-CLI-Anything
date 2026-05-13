.PHONY: build test demo clean

build:
	go build -o s02-skill-md .

test:
	go test ./...

demo: build
	@printf -- '---\nname: cli-anything-demo\ndescription: tiny demo skill\ntriggers:\n  - demo\n  - hello\n---\n# demo\n\nhello world\n' > /tmp/s02-demo.md
	./s02-skill-md parse /tmp/s02-demo.md
	@echo "--- JSON envelope ---"
	./s02-skill-md --json parse /tmp/s02-demo.md
	@rm -f /tmp/s02-demo.md

clean:
	rm -f s02-skill-md
