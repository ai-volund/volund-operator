IMG ?= ghcr.io/ai-volund/volund-operator:latest
BIN = bin/volund-operator

.PHONY: build test lint docker clean

build:
	go build -o $(BIN) ./cmd/operator

test:
	go test ./...

lint:
	golangci-lint run ./...

docker:
	docker build -t $(IMG) .

clean:
	rm -rf bin/
