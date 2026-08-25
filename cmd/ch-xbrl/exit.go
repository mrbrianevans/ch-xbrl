package main

import (
	"context"
	"errors"
)

const (
	exitOK        = 0
	exitFail      = 1
	exitUsage     = 2
	exitInterrupt = 130
)

// runExitCode is fail-closed: 0 only when the stream finished, every member
// succeeded, and at least one member was extracted. Interrupt is 130.
func runExitCode(filesOK, filesErr int64, streamErr error) int {
	if streamErr != nil {
		if errors.Is(streamErr, context.Canceled) {
			return exitInterrupt
		}
		return exitFail
	}
	if filesErr != 0 || filesOK < 1 {
		return exitFail
	}
	return exitOK
}
