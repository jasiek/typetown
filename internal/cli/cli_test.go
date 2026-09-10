package cli

import (
	"bytes"
	"strings"
	"testing"
)

// run executes the CLI with args and returns the exit code plus what was
// written to stdout and stderr.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = Run(Env{Stdout: &out, Stderr: &errOut}, args)
	return code, out.String(), errOut.String()
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string // substring; "" means stdout must be empty
		wantStderr string // substring; "" means stderr must be empty
	}{
		{
			name:       "no arguments prints usage as an error",
			args:       nil,
			wantCode:   exitUsage,
			wantStderr: "usage:",
		},
		{
			name:       "help is not an error",
			args:       []string{"help"},
			wantCode:   exitOK,
			wantStdout: "usage:",
		},
		{
			name:       "-h is not an error",
			args:       []string{"-h"},
			wantCode:   exitOK,
			wantStdout: "usage:",
		},
		{
			name:       "version prints the version",
			args:       []string{"version"},
			wantCode:   exitOK,
			wantStdout: "typetown ",
		},
		{
			name:       "version rejects extra arguments",
			args:       []string{"version", "please"},
			wantCode:   exitError,
			wantStderr: `unexpected argument "please"`,
		},
		{
			name:       "unknown command is a usage error",
			args:       []string{"nope"},
			wantCode:   exitUsage,
			wantStderr: `unknown command "nope"`,
		},
		{
			name:       "unknown flag is a usage error",
			args:       []string{"-nope"},
			wantCode:   exitUsage,
			wantStderr: "flag provided but not defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := run(t, tt.args...)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}
			if !strings.Contains(stdout, tt.wantStdout) {
				t.Errorf("stdout = %q, want it to contain %q", stdout, tt.wantStdout)
			}
			if !strings.Contains(stderr, tt.wantStderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, tt.wantStderr)
			}
			if tt.wantStdout == "" && stdout != "" {
				t.Errorf("stdout = %q, want it empty", stdout)
			}
		})
	}
}

func TestUsageListsEveryCommand(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)

	for _, c := range commands {
		if !strings.Contains(buf.String(), c.name) {
			t.Errorf("usage does not mention command %q", c.name)
		}
	}
}

func TestVersionIsNeverEmpty(t *testing.T) {
	if Version() == "" {
		t.Error("Version() = \"\", want a non-empty string")
	}
}
