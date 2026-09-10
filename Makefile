BIN     := typetown
CMD     := ./cmd/typetown
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo devel)
LDFLAGS := -X typetown/internal/cli.version=$(VERSION)

GEONAMES := https://download.geonames.org/export/dump

.PHONY: build run test lint tidy install clean sources

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

# GeoNames inputs. Not tracked in git (see .gitignore); fetched on demand.
# allCountries.zip is ~420 MB, so re-download only when it is missing.
sources: sources/allCountries.zip sources/alternateNamesV2.zip \
         sources/admin1CodesASCII.txt sources/countryInfo.txt

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
