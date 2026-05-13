.PHONY: build test demo clean

build:
	go build -o s05-repl-skin .

test:
	go test ./...

# `demo` prints an interactive usage script. The REPL is interactive by
# nature, so we don't try to drive it from `make` — we show what to type
# instead. To run for real: `./s05-repl-skin` then type the lines below.
demo: build
	@echo "Run ./s05-repl-skin and try:"
	@echo "  > :help"
	@echo "  > :skills"
	@echo "  > echo hi"
	@echo "  > :json on"
	@echo "  > echo hi"
	@echo "  > time"
	@echo "  > :history"
	@echo "  > :quit"

clean:
	rm -f s05-repl-skin
