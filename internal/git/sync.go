package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// SyncResult is what a push or pull did.
type SyncResult struct {
	Branch   string // the current branch, which was pushed or pulled
	UpToDate bool   // there was nothing to send or receive
}

var (
	ErrDetached   = errors.New("not on a branch")
	ErrNoCommits  = errors.New("the branch has no commits yet")
	ErrNoUpstream = errors.New("the branch has no upstream yet; push it first")
	ErrNoRemote   = errors.New("no remote to push to")
	// ErrRejected is a push the remote refused because it has commits the
	// branch doesn't.
	ErrRejected = errors.New("push rejected: the remote has new commits; pull first")
	// ErrDiverged is a pull that can't fast-forward because the branch and
	// its upstream both have new commits.
	ErrDiverged = errors.New("can't fast-forward: the branch and its upstream have both changed")
)

// Push sends the current branch to its upstream, as `git push` would with
// the user's config. A branch without an upstream is pushed under its own
// name to the remote git would choose, and tracks it from then on. Push
// never forces: a remote with new commits rejects it with ErrRejected.
func (r *Repo) Push(ctx context.Context) (SyncResult, error) {
	b, err := r.currentBranch(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	res := SyncResult{Branch: b.name}
	if b.oid == "" {
		return res, ErrNoCommits
	}
	args := []string{"push", "--porcelain"}
	if b.upstream == "" {
		remote, err := r.pushRemote(ctx, b.pushRemote)
		if err != nil {
			return res, err
		}
		args = append(args, "--set-upstream", "--", remote, "HEAD")
	}
	out, err := r.network(ctx, invocation{args: args})
	if err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) {
			if rej := rejection(ParsePush([]byte(gitErr.Stdout)), gitErr.Stderr); rej != nil {
				return res, rej
			}
		}
		return res, err
	}
	refs := ParsePush(out.stdout)
	res.UpToDate = len(refs) > 0 && !slices.ContainsFunc(refs, func(p PushedRef) bool { return p.Flag != '=' })
	return res, nil
}

// Pull fast-forwards the current branch to its upstream. It never merges
// or rebases, whatever pull.rebase and pull.ff say: a branch that has
// diverged fails with ErrDiverged and is left as it was.
func (r *Repo) Pull(ctx context.Context) (SyncResult, error) {
	b, err := r.currentBranch(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	res := SyncResult{Branch: b.name}
	switch {
	case b.oid == "":
		return res, ErrNoCommits
	case b.upstream == "":
		return res, ErrNoUpstream
	}
	if _, err := r.network(ctx, invocation{args: []string{"pull", "--ff-only", "--no-rebase"}}); err != nil {
		var gitErr *Error
		if errors.As(err, &gitErr) && strings.Contains(gitErr.Stderr, "Not possible to fast-forward") {
			return res, ErrDiverged
		}
		return res, err
	}
	head, err := r.read(ctx, invocation{args: []string{"rev-parse", "HEAD"}})
	if err != nil {
		return res, err
	}
	res.UpToDate = strings.TrimSpace(string(head.stdout)) == b.oid
	return res, nil
}

// branchState is the current branch as push and pull need it, read fresh
// rather than from a status that may be seconds old.
type branchState struct {
	name       string
	oid        string // empty on an unborn branch
	upstream   string // full ref name, empty without one
	pushRemote string // the remote git would push to, if configured
}

func (r *Repo) currentBranch(ctx context.Context) (branchState, error) {
	res, err := r.read(ctx, invocation{args: []string{"symbolic-ref", "-q", "--short", "HEAD"}, okCodes: []int{1}})
	if err != nil {
		return branchState{}, err
	}
	b := branchState{name: strings.TrimSpace(string(res.stdout))}
	if b.name == "" {
		return b, ErrDetached
	}
	ref := "refs/heads/" + b.name
	res, err = r.read(ctx, invocation{args: []string{
		"for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream)%00%(push:remotename)", ref,
	}})
	if err != nil {
		return b, err
	}
	// The pattern also matches refs below it; an unborn branch has no line.
	for line := range bytes.Lines(res.stdout) {
		f := strings.Split(strings.TrimSuffix(string(line), "\n"), "\x00")
		if len(f) == 4 && f[0] == ref {
			b.oid, b.upstream, b.pushRemote = f[1], f[2], f[3]
		}
	}
	return b, nil
}

// pushRemote picks the remote for a branch's first push like git does:
// the configured push remote, else origin, else the only remote.
func (r *Repo) pushRemote(ctx context.Context, configured string) (string, error) {
	if configured != "" {
		return configured, nil
	}
	res, err := r.read(ctx, invocation{args: []string{"remote"}})
	if err != nil {
		return "", err
	}
	remotes := strings.Fields(string(res.stdout))
	switch {
	case slices.Contains(remotes, "origin"):
		return "origin", nil
	case len(remotes) == 1:
		return remotes[0], nil
	case len(remotes) == 0:
		return "", ErrNoRemote
	}
	return "", fmt.Errorf("%w: there are several and none is origin; set remote.pushDefault", ErrNoRemote)
}

// PushedRef is one ref line of `git push --porcelain`.
type PushedRef struct {
	Flag    byte   // '=' up to date, ' ' fast-forward, '*' new, '!' rejected, ...
	Ref     string // "src:dst"
	Summary string // e.g. "[rejected] (fetch first)" or "abc..def"
}

// ParsePush parses `git push --porcelain` output: a "To <url>" line, one
// "<flag>\t<src>:<dst>\t<summary>" line per ref, and "Done". Other lines,
// such as the note that an upstream was set, are skipped.
func ParsePush(data []byte) []PushedRef {
	var out []PushedRef
	for line := range bytes.Lines(data) {
		f := strings.SplitN(strings.TrimSuffix(string(line), "\n"), "\t", 3)
		if len(f) != 3 || len(f[0]) != 1 {
			continue
		}
		out = append(out, PushedRef{Flag: f[0][0], Ref: f[1], Summary: f[2]})
	}
	return out
}

// rejection explains a refused push, or returns nil when no ref was
// refused. A remote that has moved on is ErrRejected; a refusal by the
// remote itself, such as a hook, quotes what the remote said.
func rejection(refs []PushedRef, stderr string) error {
	for _, p := range refs {
		if p.Flag != '!' {
			continue
		}
		if strings.Contains(p.Summary, "(fetch first)") || strings.Contains(p.Summary, "(non-fast-forward)") {
			return ErrRejected
		}
		reason := p.Summary
		if _, r, ok := strings.Cut(reason, "] "); ok {
			reason = strings.Trim(r, "()")
		}
		msg := "push rejected: " + reason
		for line := range strings.Lines(stderr) {
			if said, ok := strings.CutPrefix(line, "remote: "); ok && strings.TrimSpace(said) != "" {
				return errors.New(msg + ": " + strings.TrimSpace(said))
			}
		}
		return errors.New(msg)
	}
	return nil
}
