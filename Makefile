BIN     := typetown
CMD     := ./cmd/typetown
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -X github.com/jasiek/typetown/internal/cli.version=$(VERSION)

GEONAMES := https://download.geonames.org/export/dump
INDEX    := index
SRCFILES := sources/allCountries.zip sources/alternateNamesV2.zip \
            sources/admin1CodesASCII.txt sources/countryInfo.txt

.PHONY: build run test lint tidy install clean sources index ci

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BIN) $(CMD)

run:
	go run $(CMD)

test:
	go test ./...

lint:
	go vet ./...

# Everything CI checks, so a failure there can be reproduced in one command.
ci:
	go build ./...
	go vet ./...
	go test -race -count=1 ./...
	@test -z "$$(gofmt -l .)" || { echo "needs gofmt:"; gofmt -l .; exit 1; }
	@cp go.mod .go.mod.ci && cp go.sum .go.sum.ci && go mod tidy; \
	  ok=1; cmp -s go.mod .go.mod.ci || ok=0; cmp -s go.sum .go.sum.ci || ok=0; \
	  rm -f .go.mod.ci .go.sum.ci; \
	  test $$ok -eq 1 || { echo "go mod tidy changed go.mod or go.sum; commit the result"; exit 1; }

tidy:
	go mod tidy

install:
	go install -ldflags "$(LDFLAGS)" $(CMD)

clean:
	rm -rf bin/

# GeoNames inputs. Not tracked in git (see .gitignore); fetched on demand.
# allCountries.zip is ~420 MB, so re-download only when it is missing.
sources: $(SRCFILES)

sources/allCountries.zip:
	mkdir -p sources
	curl -fL -o $@ $(GEONAMES)/allCountries.zip

sources/alternateNamesV2.zip:
	mkdir -p sources
	curl -fL -o $@ $(GEONAMES)/alternateNamesV2.zip

sources/admin1CodesASCII.txt:
	mkdir -p sources
	curl -fL -o $@ $(GEONAMES)/admin1CodesASCII.txt

sources/countryInfo.txt:
	mkdir -p sources
	curl -fL -o $@ $(GEONAMES)/countryInfo.txt

# Fetch whatever inputs are missing, then build an index with the default
# options: every country, no population floor, alternate names included.
# Expect this to take a few minutes and produce ~250 MB in $(INDEX)/.
#
# The rule hangs off manifest.json rather than the directory: make would
# consider a directory target up to date the moment it exists, and the
# manifest is the last file Build writes. Delete $(INDEX) to force a rebuild.
index: $(INDEX)/manifest.json

$(INDEX)/manifest.json: $(SRCFILES)
	go run $(CMD) build -sources sources -out $(INDEX)
