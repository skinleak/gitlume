package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// BranchRef is a local or remote-tracking branch.
type BranchRef struct {
	Name     string // short name, e.g. "main" or "origin/main"
	Remote   bool
	Current  bool   // HEAD points at this branch
	Upstream string // short name of the upstream, if any
	Ahead    int
	Behind   int
	Gone     bool // the upstream no longer exists
	Date     time.Time
	Subject  string // subject of the tip commit
}

// Ref is the full ref name, which is never ambiguous with a file name.
func (b BranchRef) Ref() string {
	if b.Remote {
		return "refs/remotes/" + b.Name
	}
	return "refs/heads/" + b.Name
}

// branchFormat produces one NUL-separated record per line. Ref names and
// commit subjects never contain newlines.
const branchFormat = "%(refname)%00%(HEAD)%00%(upstream:short)%00" +
	"%(upstream:track,nobracket)%00%(committerdate:unix)%00%(subject)%00%(symref)"

// Branches lists local branches, then remote-tracking ones, each sorted by
// most recent commit first.
func (r *Repo) Branches(ctx context.Context) ([]BranchRef, error) {
	res, err := r.read(ctx, invocation{args: []string{
		"for-each-ref", "--sort=-committerdate", "--format=" + branchFormat,
		"refs/heads", "refs/remotes",
	}})
	if err != nil {
		return nil, err
	}
	return ParseBranches(res.stdout)
}

// ParseBranches parses `git for-each-ref --format=<branchFormat>` output.
// Symbolic refs such as origin/HEAD are skipped. Local branches come first;
// the relative order within each kind is kept.
func ParseBranches(data []byte) ([]BranchRef, error) {
	var local, remote []BranchRef
	for line := range bytes.Lines(data) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		f := strings.Split(string(line), "\x00")
		if len(f) != 7 {
			return nil, fmt.Errorf("parse branches: malformed record %q", line)
		}
		if f[6] != "" {
			continue
		}
		b := BranchRef{Current: f[1] == "*", Upstream: f[2], Subject: f[5]}
		switch {
		case strings.HasPrefix(f[0], "refs/heads/"):
			b.Name = strings.TrimPrefix(f[0], "refs/heads/")
		case strings.HasPrefix(f[0], "refs/remotes/"):
			b.Name, b.Remote = strings.TrimPrefix(f[0], "refs/remotes/"), true
		default:
			return nil, fmt.Errorf("parse branches: unexpected ref %q", f[0])
		}
		b.Ahead, b.Behind, b.Gone = parseTrack(f[3])
		if sec, err := strconv.ParseInt(f[4], 10, 64); err == nil {
			b.Date = time.Unix(sec, 0)
		}
		if b.Remote {
			remote = append(remote, b)
		} else {
			local = append(local, b)
		}
	}
	return append(local, remote...), nil
}

// parseTrack reads "ahead 1", "behind 2", "ahead 1, behind 2" or "gone".
func parseTrack(s string) (ahead, behind int, gone bool) {
	for part := range strings.SplitSeq(s, ", ") {
		key, val, _ := strings.Cut(part, " ")
		switch key {
		case "ahead":
			ahead, _ = strconv.Atoi(val)
		case "behind":
			behind, _ = strconv.Atoi(val)
		case "gone":
			gone = true
		}
	}
	return ahead, behind, gone
}

// Commit is one entry of a log.
type Commit struct {
	Hash    string // abbreviated
	Date    time.Time
	Subject string

	// Set by Graph only.
	ID      string   // full hash
	Parents []string // full hashes
	Refs    []string // full decorations, e.g. "HEAD -> refs/heads/main"
}

// Log returns the most recent n commits reachable from ref, newest first.
func (r *Repo) Log(ctx context.Context, ref string, n int) ([]Commit, error) {
	res, err := r.read(ctx, invocation{args: []string{
		// log.showSignature would add verification lines to the output.
		"log", "--no-show-signature", "-n", strconv.Itoa(n), "--no-color", "--format=%h%x00%ct%x00%s", ref, "--",
	}})
	if err != nil {
		return nil, err
	}
	return ParseLog(res.stdout)
}

// ParseLog parses `git log --format=%h%x00%ct%x00%s` output.
func ParseLog(data []byte) ([]Commit, error) {
	var out []Commit
	for line := range bytes.Lines(data) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		f := strings.SplitN(string(line), "\x00", 3)
		if len(f) != 3 || f[0] == "" {
			return nil, fmt.Errorf("parse log: malformed record %q", line)
		}
		c := Commit{Hash: f[0], Subject: f[2]}
		if sec, err := strconv.ParseInt(f[1], 10, 64); err == nil {
			c.Date = time.Unix(sec, 0)
		}
		out = append(out, c)
	}
	return out, nil
}

// graphFormat is one NUL-separated record per commit; the subject is last
// and never contains a newline.
const graphFormat = "%H%x00%h%x00%P%x00%ct%x00%D%x00%s"

// Graph returns the most recent n commits of all local and remote-tracking
// branches, and of HEAD when head is true (it can't be on an unborn
// branch), in date order with children before their parents.
func (r *Repo) Graph(ctx context.Context, n int, head bool) ([]Commit, error) {
	args := []string{
		"log", "--no-show-signature", "--date-order", "--decorate=full", "-n", strconv.Itoa(n),
		"--no-color", "--format=" + graphFormat, "--branches", "--remotes",
	}
	if head {
		args = append(args, "HEAD")
	}
	res, err := r.read(ctx, invocation{args: append(args, "--")})
	if err != nil {
		return nil, err
	}
	return ParseGraph(res.stdout)
}

// ParseGraph parses `git log --decorate=full --format=<graphFormat>` output.
func ParseGraph(data []byte) ([]Commit, error) {
	var out []Commit
	for line := range bytes.Lines(data) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		f := strings.SplitN(string(line), "\x00", 6)
		if len(f) != 6 || f[0] == "" || f[1] == "" {
			return nil, fmt.Errorf("parse graph: malformed record %q", line)
		}
		c := Commit{ID: f[0], Hash: f[1], Parents: strings.Fields(f[2]), Subject: f[5]}
		if sec, err := strconv.ParseInt(f[3], 10, 64); err == nil {
			c.Date = time.Unix(sec, 0)
		}
		if f[4] != "" {
			c.Refs = strings.Split(f[4], ", ")
		}
		out = append(out, c)
	}
	return out, nil
}

// Checkout switches to b. A remote-tracking branch gets a new local branch
// that tracks it; switching to an existing local branch of the same name is
// the caller's job. git refuses, and the error says why, when local changes
// would be overwritten.
func (r *Repo) Checkout(ctx context.Context, b BranchRef) error {
	if b.Remote {
		return r.write(ctx, invocation{args: []string{"switch", "-q", "--track", b.Name}})
	}
	return r.write(ctx, invocation{args: []string{"switch", "-q", b.Name}})
}

// Show returns a commit's header, message, diffstat and patch, as
// `git show` prints them. Refs are decorated in full (refs/heads/...) so
// callers can tell local from remote branches. statWidth is the width
// git lays the diffstat out for. Output is capped at MaxDiffBytes.
func (r *Repo) Show(ctx context.Context, hash string, statWidth int) (Diff, error) {
	args := append([]string{
		"show", "--no-show-signature", "--no-color", "--no-ext-diff", "--decorate=full", "--abbrev-commit", "-M",
		"--stat=" + strconv.Itoa(max(40, statWidth)), "--patch", "--format=medium",
	}, diffPrefixes...)
	res, err := r.read(ctx, invocation{args: append(args, hash, "--"), limit: MaxDiffBytes})
	if err != nil {
		return Diff{}, err
	}
	out := res.stdout
	if res.truncated {
		if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
			out = out[:i+1]
		}
	}
	return Diff{Text: string(out), Truncated: res.truncated}, nil
}
