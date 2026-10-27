package git

import (
	"context"
	"errors"
	"strings"
)

// ErrEmptyMessage is returned by Commit for a blank message.
var ErrEmptyMessage = errors.New("commit message is empty")

// Stage adds paths to the index. Deleted files are staged as deletions.
func (r *Repo) Stage(ctx context.Context, paths ...string) error {
	return r.write(ctx, invocation{args: append([]string{"add", "--"}, paths...)})
}

// StageAll stages every change in the work tree, including untracked files.
func (r *Repo) StageAll(ctx context.Context) error {
	return r.write(ctx, invocation{args: []string{"add", "--all"}})
}

// Unstage removes paths from the index, keeping work tree changes. It also
// works on a branch with no commits yet.
func (r *Repo) Unstage(ctx context.Context, paths ...string) error {
	return r.write(ctx, invocation{args: append([]string{"reset", "-q", "--"}, paths...)})
}

// Commit records the staged changes. The message is passed on stdin so it
// never needs shell quoting.
func (r *Repo) Commit(ctx context.Context, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return ErrEmptyMessage
	}
	return r.write(ctx, invocation{
		args:  []string{"commit", "--quiet", "--file=-"},
		stdin: strings.NewReader(message + "\n"),
	})
}
