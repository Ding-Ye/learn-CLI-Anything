.PHONY: build test demo clean

build:
	go build -o s03-skill-gen .

test:
	go test ./...

demo: build
	./s03-skill-gen demo

clean:
	rm -f s03-skill-gen
