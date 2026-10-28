package git

import (
	"bytes"
	"context"
	"os"
	"strings"
)

// MaxDiffBytes caps diff output so huge or generated files cannot stall
// the UI. Larger diffs are cut off and marked Truncated.
const MaxDiffBytes = 1 << 20

// Diff is the patch text for one change.
type Diff struct {
	Text      string
	Truncated bool
	// Note explains why there is no patch, e.g. for a nested repository.
	Note string
}

// Diff returns the patch for c: the staged diff for staged changes, the
// work tree diff for unstaged ones, and the full file for untracked ones.
func (r *Repo) Diff(ctx context.Context, c Change) (Diff, error) {
	if c.Group == Untracked && strings.HasSuffix(c.Path, "/") {
		// git lists an untracked nested repository as a directory.
		return Diff{Note: "Untracked directory (nested repository)"}, nil
	}
	res, err := r.read(ctx, diffInvocation(c))
	if err != nil {
		return Diff{}, err
	}
	out := res.stdout
	if res.truncated {
		// Cut at the last full line so no line or rune is split.
		if i := bytes.LastIndexByte(out, '\n'); i >= 0 {
			out = out[:i+1]
		}
	}
	return Diff{Text: string(out), Truncated: res.truncated}, nil
}

// diffPrefixes pin the a/ and b/ path prefixes, which diff.noprefix and
// diff.mnemonicPrefix would change, so callers can parse file names.
var diffPrefixes = []string{"--src-prefix=a/", "--dst-prefix=b/"}

func diffInvocation(c Change) invocation {
	base := append([]string{"diff", "--no-color", "--no-ext-diff"}, diffPrefixes...)
	switch c.Group {
	case Untracked:
		// --no-index exits 1 when the files differ, which they always do.
		args := append(base, "--no-index", "--", os.DevNull, c.Path)
		return invocation{args: args, limit: MaxDiffBytes, okCodes: []int{1}}
	case Staged:
		args := append(base, "--cached", "-M", "--")
		if c.OrigPath != "" {
			args = append(args, c.OrigPath)
		}
		return invocation{args: append(args, c.Path), limit: MaxDiffBytes}
	default:
		return invocation{args: append(base, "--", c.Path), limit: MaxDiffBytes}
	}
}
