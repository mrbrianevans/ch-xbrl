package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]string{"-o", "concepts.csv"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.output != "concepts.csv" || cfg.stdout {
		t.Fatalf("%+v", cfg)
	}

	cfg, err = parseConfig([]string{"--output", "concepts.csv"}, true)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.output != "concepts.csv" {
		t.Fatalf("%+v", cfg)
	}

	if _, err := parseConfig(nil, true); !errors.Is(err, errTTYStdout) {
		t.Fatalf("err = %v", err)
	}
	cfg, err = parseConfig(nil, false)
	if err != nil || !cfg.stdout {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-o", "-"}, true)
	if err != nil || !cfg.stdout {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := parseConfig([]string{"-o", "a.csv", "extra"}, false); err == nil {
		t.Fatal("expected extra-arguments error")
	}
}

func TestRun_FixtureZip(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<schema xmlns="http://www.w3.org/2001/XMLSchema" xmlns:xbrli="http://www.xbrl.org/2003/instance" targetNamespace="http://example.com/2026">
  <element name="Cash" type="xbrli:monetaryItemType" substitutionGroup="xbrli:item" xbrli:periodType="instant" xbrli:balance="debit"/>
</schema>`)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("core.xsd")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "tax.zip")
	if err := os.WriteFile(zipPath, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(buf.Bytes())
	sourcesPath := filepath.Join(dir, "sources.csv")
	src := "id,url,sha256\nfixture," + zipPath + "," + hex.EncodeToString(sum[:]) + "\n"
	if err := os.WriteFile(sourcesPath, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "concepts.csv")
	var stderr bytes.Buffer
	code := run([]string{"-o", outPath, "-sources", sourcesPath, "-cache", filepath.Join(dir, "cache")}, io.Discard, &stderr, true)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	f, err := os.Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1][1] != "Cash" || rows[1][7] != "instant" || rows[1][8] != "debit" {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0][0] != "namespace" || rows[0][1] != "local_name" {
		t.Fatalf("header = %#v", rows[0])
	}
}

func TestRun_TTYWithoutOutput(t *testing.T) {
	var stderr bytes.Buffer
	code := run(nil, io.Discard, &stderr, true)
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "-o FILE") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}
