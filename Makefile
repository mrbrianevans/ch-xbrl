.PHONY: sample taxonomy extract transform all test build

VERSION ?= 0.0.0-dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
LDFLAGS_CHXBRL := -X main.version=$(VERSION) -X main.commit=$(COMMIT)

sample:
	go run ./cmd/mksample -out samples/sample.tar.zst

taxonomy:
	go run ./cmd/taxonomy -seed-only -out reference

extract:
	go run ./cmd/ch-xbrl -o data/facts.csv samples/sample.tar.zst

transform:
	duckdb -c ".read sql/transform.sql"

all: sample taxonomy extract transform

test:
	go test ./...

build:
	go build -ldflags "$(LDFLAGS_CHXBRL)" -o bin/ch-xbrl$(EXE) ./cmd/ch-xbrl
	go build -o bin/ch-xbrl-taxonomy$(EXE) ./cmd/taxonomy
	go build -o bin/ch-xbrl-mksample$(EXE) ./cmd/mksample
