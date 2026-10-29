package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// ErrNotRepo is returned by Open when the directory is not inside a
// git work tree.
var ErrNotRepo = errors.New("not a git repository")

// ErrNoGit is returned by Open when the git binary cannot be found.
var ErrNoGit = errors.New("git executable not found in PATH")

const (
	// readTimeout bounds quick, read-only commands.
	readTimeout = 10 * time.Second
	// writeTimeout bounds commands that may run hooks, such as commit.
	writeTimeout = 5 * time.Minute
)

// Repo is a git work tree. All paths passed to and returned from its
// methods are relative to Root.
type Repo struct {
	root string
}

// Open finds the work tree containing dir.
func Open(ctx context.Context, dir string) (*Repo, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return nil, ErrNoGit
	}
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	res, err := run(ctx, dir, invocation{args: []string{"rev-parse", "--show-toplevel"}})
	if err != nil {
		// Other failures, such as "dubious ownership", carry a message
		// the user needs to see, so only this one is mapped.
		var gitErr *Error
		if errors.As(err, &gitErr) && strings.Contains(gitErr.Stderr, "not a git repository") {
			return nil, ErrNotRepo
		}
		return nil, err
	}
	root := strings.TrimSpace(string(res.stdout))
	if root == "" {
		// Inside .git or a bare repository: there is no work tree.
		return nil, ErrNotRepo
	}
	return &Repo{root: root}, nil
}

// Root is the absolute path of the work tree.
func (r *Repo) Root() string { return r.root }

// read runs a read-only command with the read timeout.
func (r *Repo) read(ctx context.Context, inv invocation) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	return run(ctx, r.root, inv)
}

// write runs a mutating command with the write timeout.
func (r *Repo) write(ctx context.Context, inv invocation) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	_, err := run(ctx, r.root, inv)
	return err
}

// network runs a command that talks to a remote. It gets the write timeout,
// since a push runs pre-push hooks, and no terminal to prompt on.
func (r *Repo) network(ctx context.Context, inv invocation) (result, error) {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	inv.detach = true
	return run(ctx, r.root, inv)
}
