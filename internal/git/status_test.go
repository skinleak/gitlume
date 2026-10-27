package git

import (
	"reflect"
	"strings"
	"testing"
)

// z joins records with NUL terminators, as `git status -z` emits them.
func z(records ...string) []byte {
	return []byte(strings.Join(records, "\x00") + "\x00")
}

const (
	oid  = "6b108b562e5a84692e6db288961157bc3c4aea13"
	hash = "78981922613b2afb6025042ff6bd878ac1994e85"
)

func TestParseStatus(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		want Status
	}{
		{
			name: "empty output",
			in:   nil,
			want: Status{},
		},
		{
			name: "clean branch with upstream",
			in: z(
				"# branch.oid "+oid,
				"# branch.head main",
				"# branch.upstream origin/main",
				"# branch.ab +2 -3",
			),
			want: Status{Branch: Branch{Head: "main", OID: oid, Upstream: "origin/main", Ahead: 2, Behind: 3}},
		},
		{
			name: "unborn branch",
			in: z(
				"# branch.oid (initial)",
				"# branch.head main",
				"1 A. N... 000000 100644 100644 0000000000000000000000000000000000000000 "+hash+" x",
			),
			want: Status{
				Branch:  Branch{Head: "main"},
				Entries: []Entry{{Kind: KindOrdinary, Index: 'A', Worktree: '.', Path: "x"}},
			},
		},
		{
			name: "detached head",
			in:   z("# branch.oid "+oid, "# branch.head (detached)"),
			want: Status{Branch: Branch{OID: oid}},
		},
		{
			name: "mixed changes with spaces, unicode and rename",
			in: z(
				"# branch.oid "+oid,
				"# branch.head main",
				"1 MM N... 100644 100644 100644 "+hash+" "+hash+" a.txt",
				"2 R. N... 100644 100644 100644 "+hash+" "+hash+" R100 new ü.txt",
				"old.txt",
				"1 .D N... 100644 100644 000000 "+hash+" "+hash+" with space.txt",
				"? d/deep.txt",
				"? untracked file.txt",
			),
			want: Status{
				Branch: Branch{Head: "main", OID: oid},
				Entries: []Entry{
					{Kind: KindOrdinary, Index: 'M', Worktree: 'M', Path: "a.txt"},
					{Kind: KindRenamed, Index: 'R', Worktree: '.', Path: "new ü.txt", OrigPath: "old.txt"},
					{Kind: KindOrdinary, Index: '.', Worktree: 'D', Path: "with space.txt"},
					{Kind: KindUntracked, Index: '?', Worktree: '?', Path: "d/deep.txt"},
					{Kind: KindUntracked, Index: '?', Worktree: '?', Path: "untracked file.txt"},
				},
			},
		},
		{
			name: "merge conflict",
			in: z(
				"u UU N... 100644 100644 100644 100644 " + hash + " " + hash + " " + hash + " f",
			),
			want: Status{Entries: []Entry{{Kind: KindUnmerged, Index: 'U', Worktree: 'U', Path: "f"}}},
		},
		{
			name: "unknown header is ignored",
			in:   z("# branch.future something", "# stash 3"),
			want: Status{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseStatus(tt.in)
			if err != nil {
				t.Fatalf("ParseStatus: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseStatus\n got: %+v\nwant: %+v", got, tt.want)
			}
		})
	}
}

func TestParseStatusErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"truncated ordinary record", z("1 M. N... 100644")},
		{"rename without source path", []byte("2 R. N... 100644 100644 100644 " + hash + " " + hash + " R100 new")},
		{"unknown record type", z("x what")},
		{"bad XY field", z("1 MMM N... 100644 100644 100644 " + hash + " " + hash + " a")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ParseStatus(tt.in); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestStatusChanges(t *testing.T) {
	st := Status{Entries: []Entry{
		{Kind: KindOrdinary, Index: 'M', Worktree: 'M', Path: "both"},
		{Kind: KindOrdinary, Index: 'A', Worktree: '.', Path: "added"},
		{Kind: KindOrdinary, Index: '.', Worktree: 'D', Path: "deleted"},
		{Kind: KindRenamed, Index: 'R', Worktree: 'M', Path: "new", OrigPath: "old"},
		{Kind: KindUnmerged, Index: 'U', Worktree: 'U', Path: "conflict"},
		{Kind: KindUntracked, Index: '?', Worktree: '?', Path: "fresh"},
	}}

	tests := []struct {
		group Group
		want  []Change
	}{
		{Staged, []Change{
			{Group: Staged, Code: 'M', Path: "both"},
			{Group: Staged, Code: 'A', Path: "added"},
			{Group: Staged, Code: 'R', Path: "new", OrigPath: "old"},
		}},
		{Unstaged, []Change{
			{Group: Unstaged, Code: 'M', Path: "both"},
			{Group: Unstaged, Code: 'D', Path: "deleted"},
			{Group: Unstaged, Code: 'M', Path: "new"},
			{Group: Unstaged, Code: 'U', Path: "conflict"},
		}},
		{Untracked, []Change{
			{Group: Untracked, Code: '?', Path: "fresh"},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.group.String(), func(t *testing.T) {
			if got := st.Changes(tt.group); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Changes(%v)\n got: %+v\nwant: %+v", tt.group, got, tt.want)
			}
		})
	}
}
