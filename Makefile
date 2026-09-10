BIN     := typetown
CMD     := ./cmd/typetown
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -X typetown/internal/cli.version=$(VERSION)

.PHONY: build run test lint tidy install clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(CMD)

run:
	go run $(CMD)

test:
	go test ./...

lint:
	go vet ./...

tidy:
	go mod tidy

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

clean:
	rm -rf bin/
