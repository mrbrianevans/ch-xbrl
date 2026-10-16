package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

// conceptsCSV is the concept lookup written into the -o directory.
// The name is fixed. printUsage lists every file the tool writes;
// add a name there when the directory grows.
const conceptsCSV = "concepts.csv"

// ensureOutputDir creates path, including parents, when it is missing.
// An existing directory is left as it is, empty or not. A symlink to a
// directory is followed. Anything else that already exists is an error.
func ensureOutputDir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return fmt.Errorf("create output directory: %w", err)
			}
			return nil
		}
		return fmt.Errorf("output directory: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("output directory: %w", err)
		}
	}
	if !info.IsDir() {
		return fmt.Errorf("output path %s exists and is not a directory", path)
	}
	return nil
}

// writeAtomic replaces dir/name with the bytes write produces.
// The previous file stays in place until the new one is complete.
// Rename replaces a symlink in dir without writing through it.
// A directory at name is an error. Other files in dir are not touched.
func writeAtomic(dir, name string, write func(io.Writer) error) error {
	final := filepath.Join(dir, name)
	info, err := os.Lstat(final)
	switch {
	case err == nil && info.IsDir():
		return fmt.Errorf("%s is a directory", final)
	case err != nil && !os.IsNotExist(err):
		return fmt.Errorf("output file: %w", err)
	}

	f, err := os.CreateTemp(dir, "."+name+".*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmp := f.Name()
	done := false
	defer func() {
		if !done {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Chmod(0o644); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replaceFile(tmp, final); err != nil {
		return fmt.Errorf("replace %s: %w", final, err)
	}
	done = true
	return nil
}

func replaceFile(tmp, final string) error {
	err := os.Rename(tmp, final)
	if err == nil || runtime.GOOS != "windows" {
		if err != nil {
			os.Remove(tmp)
		}
		return err
	}
	if rmErr := os.Remove(final); rmErr != nil && !os.IsNotExist(rmErr) {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, final); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
