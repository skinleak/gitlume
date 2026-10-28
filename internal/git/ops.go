package git

import (
	"bytes"
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

// ErrNestedRepo is returned by Discard for an untracked nested repository,
// which is never deleted from gitlume.
var ErrNestedRepo = errors.New("refusing to delete a nested repository")

// Discard throws away every uncommitted change to c, staged and unstaged.
// Tracked paths are restored to HEAD, and paths that HEAD does not have
// are removed from the index and the work tree. Untracked files are
// deleted. extra lists further paths to restore together with c, such as
// the source path of a rename.
func (r *Repo) Discard(ctx context.Context, c Change, extra ...string) error {
	if c.Group == Untracked {
		if strings.HasSuffix(c.Path, "/") {
			return ErrNestedRepo
		}
		return r.write(ctx, invocation{args: []string{"clean", "-f", "-q", "--", c.Path}})
	}
	paths := append([]string{c.Path}, extra...)
	unborn, err := r.unborn(ctx)
	if err != nil {
		return err
	}
	if unborn {
		// There is no HEAD to restore from: every tracked file is new.
		return r.write(ctx, invocation{args: append([]string{"rm", "-f", "-q", "--"}, paths...)})
	}
	return r.write(ctx, invocation{args: append(
		[]string{"restore", "--source=HEAD", "--staged", "--worktree", "--"}, paths...)})
}

// DiscardAll throws away every uncommitted change: the index and work tree
// are reset to HEAD and untracked files and directories are deleted.
// Ignored files and nested repositories are kept.
func (r *Repo) DiscardAll(ctx context.Context) error {
	if err := r.write(ctx, invocation{args: []string{"reset", "--hard", "-q"}}); err != nil {
		return err
	}
	return r.write(ctx, invocation{args: []string{"clean", "-f", "-d", "-q"}})
}

// unborn reports whether HEAD has no commit yet.
func (r *Repo) unborn(ctx context.Context) (bool, error) {
	res, err := r.read(ctx, invocation{
		args:    []string{"rev-parse", "--verify", "-q", "HEAD"},
		okCodes: []int{1},
	})
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(res.stdout)) == 0, nil
}
