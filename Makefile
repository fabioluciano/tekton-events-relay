.PHONY: build test vet fmt run docker clean

BIN ?= bin/tekton-events-relay
IMG ?= tekton-events-relay:dev

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN) ./cmd/receiver

test:
	go test -race -cover ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

run: build
	./$(BIN) --config wiki/examples/config.yaml

docker:
	docker build -t $(IMG) .

clean:
	rm -rf bin/
