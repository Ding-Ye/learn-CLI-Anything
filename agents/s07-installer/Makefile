.PHONY: build test demo clean

build:
	go build -o s07-installer .

test:
	go test ./...

# demo: install the bundled fixture tarball from testdata/ via a tiny inline
# HTTP server (started from main_test.go would be circular — we just write a
# manifest pointing at a file:// URL and let the installer handle it).
#
# We use python3's http.server because every dev box has it; if you don't,
# `make test` already exercises the bundled path through httptest.
demo: build
	@mkdir -p .demo
	@cp testdata/anygen-0.1.tar.gz .demo/
	@( cd .demo && python3 -m http.server 8087 >/dev/null 2>&1 & echo $$! > .server.pid )
	@sleep 1
	@printf '{"name":"anygen","version":"0.1","backend":"bundled","entry":"anygen","skill":"SKILL.md","url":"http://127.0.0.1:8087/anygen-0.1.tar.gz"}' > .demo/manifest.json
	@echo "--- install ---"
	@./s07-installer install .demo/manifest.json || true
	@echo "--- list ---"
	@./s07-installer --json list
	@echo "--- uninstall ---"
	@./s07-installer uninstall anygen || true
	@kill `cat .demo/.server.pid` 2>/dev/null || true
	@rm -rf .demo

clean:
	rm -f s07-installer
	rm -rf .demo
