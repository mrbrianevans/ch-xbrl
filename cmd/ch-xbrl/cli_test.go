package main

import (
	"errors"
	"flag"
	"runtime"
	"testing"
)

func TestParseConfig(t *testing.T) {
	t.Parallel()

	t.Run("positional with -o file", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseConfig([]string{"-o", "facts.csv", "archive.zip"}, true)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.input != "archive.zip" || cfg.output != "facts.csv" || cfg.stdout || cfg.workers != runtime.NumCPU() {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("long --output", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseConfig([]string{"--output", "out.csv", "https://example/a.zip"}, false)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.input != "https://example/a.zip" || cfg.output != "out.csv" || cfg.stdout {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("omit -o on TTY", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfig([]string{"archive.zip"}, true)
		if !errors.Is(err, errTTYStdout) {
			t.Fatalf("err = %v, want errTTYStdout", err)
		}
	})

	t.Run("omit -o when not TTY", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseConfig([]string{"archive.zip"}, false)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if !cfg.stdout || cfg.output != "" || cfg.input != "archive.zip" {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("-o - forces stdout on TTY", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseConfig([]string{"-o", "-", "archive.zip"}, true)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if !cfg.stdout || cfg.output != "" {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("missing input", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfig([]string{"-o", "facts.csv"}, false)
		if !errors.Is(err, errMissingInput) {
			t.Fatalf("err = %v, want errMissingInput", err)
		}
	})

	t.Run("extra arguments", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfig([]string{"-o", "out.csv", "a.zip", "b.zip"}, false)
		if err == nil {
			t.Fatal("expected extra-arguments error")
		}
	})

	t.Run("workers clamp", func(t *testing.T) {
		t.Parallel()
		cfg, err := parseConfig([]string{"-workers", "0", "-o", "out.csv", "a.zip"}, true)
		if err != nil {
			t.Fatalf("parseConfig: %v", err)
		}
		if cfg.workers != 1 {
			t.Fatalf("workers = %d, want 1", cfg.workers)
		}
	})

	t.Run("help", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfig([]string{"-h"}, false)
		if !errors.Is(err, flag.ErrHelp) {
			t.Fatalf("err = %v, want flag.ErrHelp", err)
		}
	})

	t.Run("old -in flag is rejected", func(t *testing.T) {
		t.Parallel()
		_, err := parseConfig([]string{"-in", "a.zip"}, false)
		if err == nil {
			t.Fatal("expected unknown-flag error for -in")
		}
	})
}
