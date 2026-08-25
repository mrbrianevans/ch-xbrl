package main

import (
	"fmt"
	"runtime/debug"
)

// version and commit are set at link time, e.g.
//
//	go build -ldflags "-X main.version=v1.0.0 -X main.commit=abc1234"
//
// Untagged / go run builds keep version 0.0.0-dev and fill commit from VCS
// metadata embedded by the Go toolchain when available.
var (
	version = "0.0.0-dev"
	commit  = ""
)

func versionLine() string {
	c := commit
	if c == "" {
		c = vcsRevision()
	}
	if c == "" {
		return "ch-xbrl " + version
	}
	return fmt.Sprintf("ch-xbrl %s (%s)", version, c)
}

func vcsRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > 7 {
				return s.Value[:7]
			}
			return s.Value
		}
	}
	return ""
}
