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

// ErrAmendPushed is returned when the last commit is already on the
// branch's upstream: amending it would need a force push, which gitlume
// never does.
var ErrAmendPushed = errors.New("the last commit is already pushed; amending it would need a force push")

// ErrAmendEmpty is returned when the amended commit would have no changes,
// for instance because the staged changes undo it. git refuses that, as it
// is rarely what was meant.
var ErrAmendEmpty = errors.New("amending would leave the last commit with no changes")

// AmendSubject returns the subject of the last commit, to edit before
// Amend, or why it can't be amended.
func (r *Repo) AmendSubject(ctx context.Context) (string, error) {
	subject, _, err := r.amendable(ctx)
	return subject, err
}

// Amend replaces the last commit with one that also has the staged
// changes and the given subject. The rest of the message, such as a body
// or trailers, is kept: the prompt edits only the subject line. Hooks run
// as for a commit.
func (r *Repo) Amend(ctx context.Context, subject string) error {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return ErrEmptyMessage
	}
	_, body, err := r.amendable(ctx)
	if err != nil {
		return err
	}
	message := subject
	if body = strings.TrimRight(body, "\n"); strings.TrimSpace(body) != "" {
		message += "\n\n" + body
	}
	err = r.write(ctx, invocation{
		args:  []string{"commit", "--amend", "--quiet", "--file=-"},
		stdin: strings.NewReader(message + "\n"),
	})
	// git spreads this explanation over several lines; say it in one.
	var gitErr *Error
	if errors.As(err, &gitErr) && strings.Contains(gitErr.Stderr, "would make\nit empty") {
		return ErrAmendEmpty
	}
	return err
}

// amendable reads the last commit's subject and body, read fresh so a
// commit pushed meanwhile is never amended. A detached HEAD has no
// upstream to compare with and can always be amended.
func (r *Repo) amendable(ctx context.Context) (subject, body string, err error) {
	b, err := r.currentBranch(ctx)
	switch {
	case errors.Is(err, ErrDetached):
	case err != nil:
		return "", "", err
	case b.oid == "":
		return "", "", ErrNoCommits
	case b.upstream != "":
		// HEAD is pushed when the upstream contains it: nothing is ahead.
		res, err := r.read(ctx, invocation{args: []string{"rev-list", "--count", b.upstream + "..HEAD", "--"}})
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(string(res.stdout)) == "0" {
			return "", "", ErrAmendPushed
		}
	}
	// log.showSignature would add verification lines to the output.
	res, err := r.read(ctx, invocation{args: []string{"log", "--no-show-signature", "-1", "--format=%s%x00%b", "HEAD", "--"}})
	if err != nil {
		return "", "", err
	}
	subject, body, _ = strings.Cut(strings.TrimSuffix(string(res.stdout), "\n"), "\x00")
	return subject, body, nil
}
