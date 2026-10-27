package git

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Group is the section of the status view a change belongs to.
type Group int

const (
	Staged Group = iota
	Unstaged
	Untracked
)

func (g Group) String() string {
	switch g {
	case Staged:
		return "Staged"
	case Unstaged:
		return "Unstaged"
	case Untracked:
		return "Untracked"
	}
	return "Unknown"
}

// EntryKind is the porcelain v2 record type of an entry.
type EntryKind int

const (
	KindOrdinary EntryKind = iota
	KindRenamed            // rename or copy
	KindUnmerged
	KindUntracked
)

// Entry is one path reported by git status.
type Entry struct {
	Kind     EntryKind
	Index    byte   // X: status in the index ('.' when unchanged)
	Worktree byte   // Y: status in the work tree ('.' when unchanged)
	Path     string // path relative to the repository root
	OrigPath string // source path of a rename or copy
}

// Branch describes HEAD and its upstream.
type Branch struct {
	Head     string // branch name; empty when detached
	OID      string // commit id; empty on an unborn branch
	Upstream string
	Ahead    int
	Behind   int
}

// Detached reports whether HEAD is not on a branch.
func (b Branch) Detached() bool { return b.Head == "" }

// Unborn reports whether the branch has no commits yet.
func (b Branch) Unborn() bool { return b.OID == "" }

// Status is a parsed snapshot of git status.
type Status struct {
	Branch  Branch
	Entries []Entry
}

// Change is an entry as it appears in one group of the status view. An
// entry with both staged and unstaged changes yields two Changes.
type Change struct {
	Group    Group
	Code     byte // single status letter shown next to the path
	Path     string
	OrigPath string
}

// Changes returns the changes in g, in git's order.
func (s Status) Changes(g Group) []Change {
	var out []Change
	for _, e := range s.Entries {
		tracked := e.Kind == KindOrdinary || e.Kind == KindRenamed
		switch {
		case g == Untracked && e.Kind == KindUntracked:
			out = append(out, Change{Group: g, Code: '?', Path: e.Path})
		case g == Unstaged && e.Kind == KindUnmerged:
			out = append(out, Change{Group: g, Code: 'U', Path: e.Path})
		case g == Staged && tracked && e.Index != '.':
			out = append(out, Change{Group: g, Code: e.Index, Path: e.Path, OrigPath: e.OrigPath})
		case g == Unstaged && tracked && e.Worktree != '.':
			out = append(out, Change{Group: g, Code: e.Worktree, Path: e.Path})
		}
	}
	return out
}

// Status runs git status and parses the result.
func (r *Repo) Status(ctx context.Context) (Status, error) {
	res, err := r.read(ctx, invocation{args: []string{
		"status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all",
	}})
	if err != nil {
		return Status{}, err
	}
	return ParseStatus(res.stdout)
}

// ParseStatus parses `git status --porcelain=v2 -z --branch` output.
func ParseStatus(data []byte) (Status, error) {
	var st Status
	fields := bytes.Split(data, []byte{0})
	for i := 0; i < len(fields); i++ {
		rec := string(fields[i])
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '#':
			parseHeader(&st.Branch, rec)
		case '1':
			e, err := parseOrdinary(rec)
			if err != nil {
				return Status{}, err
			}
			st.Entries = append(st.Entries, e)
		case '2':
			// The source path of a rename follows as its own field.
			if i+1 >= len(fields) {
				return Status{}, fmt.Errorf("parse status: rename without source path: %q", rec)
			}
			i++
			e, err := parseRenamed(rec, string(fields[i]))
			if err != nil {
				return Status{}, err
			}
			st.Entries = append(st.Entries, e)
		case 'u':
			e, err := parseUnmerged(rec)
			if err != nil {
				return Status{}, err
			}
			st.Entries = append(st.Entries, e)
		case '?':
			st.Entries = append(st.Entries, Entry{
				Kind: KindUntracked, Index: '?', Worktree: '?', Path: strings.TrimPrefix(rec, "? "),
			})
		case '!':
			// Ignored files are not requested; skip defensively.
		default:
			return Status{}, fmt.Errorf("parse status: unknown record %q", rec)
		}
	}
	return st, nil
}

// parseHeader fills b from a "# branch.*" line. Unknown headers are ignored
// so newer git versions do not break parsing.
func parseHeader(b *Branch, rec string) {
	key, val, _ := strings.Cut(strings.TrimPrefix(rec, "# "), " ")
	switch key {
	case "branch.oid":
		if val != "(initial)" {
			b.OID = val
		}
	case "branch.head":
		if val != "(detached)" {
			b.Head = val
		}
	case "branch.upstream":
		b.Upstream = val
	case "branch.ab":
		a, bh, _ := strings.Cut(val, " ")
		b.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
		b.Behind, _ = strconv.Atoi(strings.TrimPrefix(bh, "-"))
	}
}

// splitRecord splits rec into n space-separated fields; the last field is
// the path and may itself contain spaces.
func splitRecord(rec string, n int) ([]string, error) {
	parts := strings.SplitN(rec, " ", n)
	if len(parts) != n || len(parts[1]) != 2 || parts[n-1] == "" {
		return nil, fmt.Errorf("parse status: malformed record %q", rec)
	}
	return parts, nil
}

// "1 XY sub mH mI mW hH hI path"
func parseOrdinary(rec string) (Entry, error) {
	p, err := splitRecord(rec, 9)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Kind: KindOrdinary, Index: p[1][0], Worktree: p[1][1], Path: p[8]}, nil
}

// "2 XY sub mH mI mW hH hI Xscore path" followed by the source path.
func parseRenamed(rec, orig string) (Entry, error) {
	p, err := splitRecord(rec, 10)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Kind: KindRenamed, Index: p[1][0], Worktree: p[1][1], Path: p[9], OrigPath: orig}, nil
}

// "u XY sub m1 m2 m3 mW h1 h2 h3 path"
func parseUnmerged(rec string) (Entry, error) {
	p, err := splitRecord(rec, 11)
	if err != nil {
		return Entry{}, err
	}
	return Entry{Kind: KindUnmerged, Index: p[1][0], Worktree: p[1][1], Path: p[10]}, nil
}
