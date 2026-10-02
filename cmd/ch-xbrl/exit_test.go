package main

import (
	"context"
	"errors"
	"testing"
)

func TestRunExitCode(t *testing.T) {
	t.Parallel()

	streamFail := errors.New("stream exploded")
	cases := []struct {
		name            string
		ok, errN        int64
		streamErr       error
		continueOnError bool
		want            int
	}{
		{name: "clean run", ok: 10, want: exitOK},
		{name: "member parse error", ok: 9, errN: 1, want: exitFail},
		{name: "only member errors", errN: 3, want: exitFail},
		{name: "empty archive", want: exitFail},
		{name: "stream error", ok: 10, streamErr: streamFail, want: exitFail},
		{name: "stream error and member errors", ok: 1, errN: 2, streamErr: streamFail, want: exitFail},
		{name: "interrupt", ok: 4, streamErr: context.Canceled, want: exitInterrupt},
		{name: "interrupt wrapping", ok: 1, errN: 1, streamErr: errors.Join(context.Canceled), want: exitInterrupt},
		{name: "continue-on-error with member errors", ok: 9, errN: 1, continueOnError: true, want: exitOK},
		{name: "continue-on-error all members failed", errN: 3, continueOnError: true, want: exitFail},
		{name: "continue-on-error empty archive", continueOnError: true, want: exitFail},
		{name: "continue-on-error stream error", ok: 10, streamErr: streamFail, continueOnError: true, want: exitFail},
		{name: "continue-on-error interrupt", ok: 4, streamErr: context.Canceled, continueOnError: true, want: exitInterrupt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := runExitCode(tc.ok, tc.errN, tc.streamErr, tc.continueOnError)
			if got != tc.want {
				t.Fatalf("runExitCode(%d, %d, %v, %v) = %d, want %d",
					tc.ok, tc.errN, tc.streamErr, tc.continueOnError, got, tc.want)
			}
		})
	}
}
