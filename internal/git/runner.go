// Package git is a thin, UI-free wrapper around the git CLI. Commands are
// run with a fixed environment so their output is stable and parseable,
// and every call takes a context so callers control cancellation.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// globalArgs are prepended to every invocation so user config cannot
// change the output format we parse.
var globalArgs = []string{
	"-c", "color.ui=false",
	"-c", "core.quotepath=false",
}

// fixedEnv makes git output stable and non-interactive. Optional locks are
// disabled so a background refresh never contends with the user's editor
// or another git process for index.lock. Pathspecs are literal so file
// names containing '*', '?', '[' or ':(' are never treated as patterns.
var fixedEnv = []string{
	"LC_ALL=C",
	"GIT_TERMINAL_PROMPT=0",
	"GIT_OPTIONAL_LOCKS=0",
	"GIT_LITERAL_PATHSPECS=1",
}

// waitDelay bounds how long we wait for output pipes after git exits. A
// hook may leave a background process holding stdout open; without this
// the call would never return.
const waitDelay = time.Second

// Error is returned when git exits unsuccessfully.
type Error struct {
	Args     []string
	ExitCode int
	Stderr   string
	Stdout   string
}

func (e *Error) Error() string {
	sub := "git"
	if len(e.Args) > 0 {
		sub = "git " + e.Args[0]
	}
	msg := summary(e.Stderr)
	if msg == "" {
		// Some failures, like "nothing to commit", are reported on stdout,
		// where git puts the conclusion last.
		msg = lastLine(e.Stdout)
	}
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.ExitCode)
	}
	return sub + ": " + msg
}

// summary picks the most useful line of git output: the first "fatal:" or
// "error:" line, or else the first non-empty line.
func summary(s string) string {
	first := ""
	for line := range strings.Lines(s) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "fatal:") || strings.HasPrefix(t, "error:") {
			return t
		}
		if first == "" {
			first = t
		}
	}
	return first
}

// lastLine returns the last non-empty line of s, trimmed.
func lastLine(s string) string {
	s = strings.TrimSpace(s)
	return strings.TrimSpace(s[strings.LastIndexByte(s, '\n')+1:])
}

// invocation describes a single git run.
type invocation struct {
	args    []string
	stdin   io.Reader
	limit   int   // max stdout bytes kept; 0 means unlimited
	okCodes []int // non-zero exit codes that are not errors
}

// result is the output of a git run.
type result struct {
	stdout    []byte
	truncated bool
}

// run executes git in dir. When inv.limit is exceeded, git is stopped
// early and the output is marked truncated rather than failing.
func run(ctx context.Context, dir string, inv invocation) (result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stdout := &cappedBuffer{limit: inv.limit, onFull: cancel}
	var stderr bytes.Buffer

	cmd := exec.CommandContext(ctx, "git", append(globalArgs, inv.args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), fixedEnv...)
	cmd.Stdin = inv.stdin
	cmd.Stdout = stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay

	err := cmd.Run()
	if stdout.truncated {
		return result{stdout: stdout.Bytes(), truncated: true}, nil
	}
	if err == nil {
		return result{stdout: stdout.Bytes()}, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result{}, fmt.Errorf("git %s: %w", inv.args[0], ctxErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		if errors.Is(err, exec.ErrWaitDelay) {
			// git finished; only a leftover child kept the pipes open.
			return result{stdout: stdout.Bytes()}, nil
		}
		return result{}, fmt.Errorf("git %s: %w", inv.args[0], err)
	}
	code := exitErr.ExitCode()
	for _, ok := range inv.okCodes {
		if code == ok {
			return result{stdout: stdout.Bytes()}, nil
		}
	}
	return result{}, &Error{
		Args:     inv.args,
		ExitCode: code,
		Stderr:   stderr.String(),
		Stdout:   stdout.String(),
	}
}

// cappedBuffer keeps at most limit bytes (unlimited when limit <= 0) and
// calls onFull once when more arrive, so the producer can be stopped.
// The buffer is a named field, not embedded: an embedded bytes.Buffer
// would expose ReadFrom, which io.Copy prefers over Write, bypassing the cap.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
	onFull    func()
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit <= 0 {
		return b.buf.Write(p)
	}
	if b.truncated {
		return len(p), nil
	}
	if room := b.limit - b.buf.Len(); len(p) > room {
		b.buf.Write(p[:room])
		b.truncated = true
		b.onFull()
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *cappedBuffer) String() string { return b.buf.String() }
