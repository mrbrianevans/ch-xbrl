.PHONY: sample taxonomy extract transform all test build

sample:
	go run ./cmd/mksample -out samples/sample.tar.zst

taxonomy:
	go run ./cmd/taxonomy -seed-only -out reference

extract:
	go run ./cmd/ch-xbrl -in samples/sample.tar.zst -out data/facts.csv

transform:
	duckdb -c ".read sql/transform.sql"

all: sample taxonomy extract transform

test:
	go test ./...

build:
	go build -o bin/ch-xbrl$(EXE) ./cmd/ch-xbrl
	go build -o bin/ch-xbrl-taxonomy$(EXE) ./cmd/taxonomy
	go build -o bin/ch-xbrl-mksample$(EXE) ./cmd/mksample
