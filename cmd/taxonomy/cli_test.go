package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	t.Parallel()

	cfg, err := parseConfig([]string{"-o", "my-dir"})
	if err != nil || cfg.output != "my-dir" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseConfig([]string{"--output", "my-dir/"})
	if err != nil || cfg.output != "my-dir" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	if _, err := parseConfig(nil); !errors.Is(err, errMissingOutput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := parseConfig([]string{"-o", "-"}); !errors.Is(err, errStdoutOutput) {
		t.Fatalf("err = %v", err)
	}
	if _, err := parseConfig([]string{"-o", "my-dir", "extra"}); err == nil {
		t.Fatal("expected extra-arguments error")
	}

	cfg, err = parseConfig([]string{"-o", "out", "-c", "/tmp/cache", "-s", "sources.csv"})
	if err != nil || cfg.cache != "/tmp/cache" || cfg.sources != "sources.csv" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-o", "out", "--cache", "/tmp/cache", "--sources", "sources.csv"})
	if err != nil || cfg.cache != "/tmp/cache" || cfg.sources != "sources.csv" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	cfg, err = parseConfig([]string{"-o", "out", "-cache", "/tmp/cache", "-sources", "sources.csv"})
	if err != nil || cfg.cache != "/tmp/cache" || cfg.sources != "sources.csv" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestRun_FixtureZip(t *testing.T) {
	dir := t.TempDir()
	sourcesPath := writeFixtureSources(t, dir)
	outDir := filepath.Join(dir, "nested", "out")
	var stderr bytes.Buffer
	code := run([]string{"-o", outDir + string(os.PathSeparator), "-s", sourcesPath, "-c", filepath.Join(dir, "cache")}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	rows := readConcepts(t, filepath.Join(outDir, conceptsCSV))
	if len(rows) != 2 || rows[1][1] != "Cash" || rows[1][7] != "instant" || rows[1][8] != "debit" {
		t.Fatalf("rows = %#v", rows)
	}
	if rows[0][0] != "namespace" || rows[0][1] != "local_name" {
		t.Fatalf("header = %#v", rows[0])
	}
	if !strings.Contains(stderr.String(), filepath.Join(outDir, conceptsCSV)) {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRun_CreatesParentsAndKeepsSiblings(t *testing.T) {
	dir := t.TempDir()
	sourcesPath := writeFixtureSources(t, dir)
	outDir := filepath.Join(dir, "a", "b")
	sibling := filepath.Join(outDir, "notes.txt")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, conceptsCSV), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-o", outDir, "-s", sourcesPath, "-c", filepath.Join(dir, "cache")}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	body, err := os.ReadFile(sibling)
	if err != nil || string(body) != "keep" {
		t.Fatalf("sibling = %q err=%v", body, err)
	}
	rows := readConcepts(t, filepath.Join(outDir, conceptsCSV))
	if len(rows) != 2 || rows[0][0] != "namespace" {
		t.Fatalf("rows = %#v", rows)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, ".concepts.csv.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temp files left behind: %v", matches)
	}
}

func TestRun_OutputPathIsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-o", path}, &stderr)
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "not a directory") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "x" {
		t.Fatalf("file changed: %q err=%v", body, err)
	}
}

func TestRun_ConceptsCSVIsDirectory(t *testing.T) {
	dir := t.TempDir()
	sourcesPath := writeFixtureSources(t, dir)
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(filepath.Join(outDir, conceptsCSV), 0o755); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-o", outDir, "-s", sourcesPath, "-c", filepath.Join(dir, "cache")}, &stderr)
	if code != 1 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "is a directory") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func TestRun_ReplacesConceptsSymlink(t *testing.T) {
	dir := t.TempDir()
	sourcesPath := writeFixtureSources(t, dir)
	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere.csv")
	if err := os.WriteFile(target, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(outDir, conceptsCSV)); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-o", outDir, "-s", sourcesPath, "-c", filepath.Join(dir, "cache")}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	outside, err := os.ReadFile(target)
	if err != nil || string(outside) != "outside" {
		t.Fatalf("symlink target changed: %q err=%v", outside, err)
	}
	info, err := os.Lstat(filepath.Join(outDir, conceptsCSV))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("concepts.csv is still a symlink")
	}
	rows := readConcepts(t, filepath.Join(outDir, conceptsCSV))
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
}

func TestRun_FollowsOutputSymlink(t *testing.T) {
	dir := t.TempDir()
	sourcesPath := writeFixtureSources(t, dir)
	target := filepath.Join(dir, "real")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := run([]string{"-o", link, "-s", sourcesPath, "-c", filepath.Join(dir, "cache")}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	rows := readConcepts(t, filepath.Join(target, conceptsCSV))
	if len(rows) != 2 {
		t.Fatalf("rows = %#v", rows)
	}
	if _, err := os.Lstat(filepath.Join(link, conceptsCSV)); err != nil {
		t.Fatal(err)
	}
}

func TestRun_Help(t *testing.T) {
	var stderr bytes.Buffer
	code := run([]string{"-h"}, &stderr)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, want := range []string{
		"-o, --output DIR",
		"-c, --cache DIR",
		"-s, --sources FILE",
		"concepts.csv",
		"parent directories",
		"Every other file in DIR is left",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("usage missing %q:\n%s", want, stderr.String())
		}
	}
}

func TestRun_MissingOutput(t *testing.T) {
	var stderr bytes.Buffer
	code := run(nil, &stderr)
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stderr.String(), "-o DIR") {
		t.Fatalf("stderr = %s", stderr.String())
	}
}

func writeFixtureSources(t *testing.T, dir string) string {
	t.Helper()
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
	return sourcesPath
}

func readConcepts(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode = %o", info.Mode().Perm())
	}
	return rows
}
