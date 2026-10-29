package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/skinleak/qitlog/internal/ui/theme"
)

// Captured from `git diff --cached -M --src-prefix=a/ --dst-prefix=b/` in a
// temp repo with a new, a deleted, a binary, a modified (losing a line that
// starts with "--"), a renamed and edited, and a chmodded file.
const patchFixture = `diff --git a/add.txt b/add.txt
new file mode 100644
index 0000000..45b983b
--- /dev/null
+++ b/add.txt
@@ -0,0 +1 @@
+hi
diff --git a/del.txt b/del.txt
deleted file mode 100644
index 286c5f5..0000000
--- a/del.txt
+++ /dev/null
@@ -1 +0,0 @@
-gone
diff --git a/img.bin b/img.bin
index bdc955b..8835708 100644
Binary files a/img.bin and b/img.bin differ
diff --git a/mod.sql b/mod.sql
index 6cb5c6c..de98044 100644
--- a/mod.sql
+++ b/mod.sql
@@ -1,3 +1,3 @@
 a
--- comment
 b
+c
diff --git a/old.txt b/new.txt
similarity index 79%
rename from old.txt
rename to new.txt
index f384549..b2f931a 100644
--- a/old.txt
+++ b/new.txt
@@ -2,3 +2,4 @@ one
 two
 three
 four
+five
diff --git a/run.sh b/run.sh
old mode 100644
new mode 100755
`

func TestTidyPatch(t *testing.T) {
	lines, heads := tidyPatch(splitDiff(patchFixture), false)
	wantLines := []string{
		"add.txt", "@@ -0,0 +1 @@", "+hi", "",
		"del.txt", "@@ -1 +0,0 @@", "-gone", "",
		"img.bin", "",
		"mod.sql", "@@ -1,3 +1,3 @@", " a", "--- comment", " b", "+c", "",
		"new.txt", "@@ -2,3 +2,4 @@ one", " two", " three", " four", "+five", "",
		"run.sh",
	}
	if !reflect.DeepEqual(lines, wantLines) {
		t.Errorf("lines\n got: %q\nwant: %q", lines, wantLines)
	}
	wantHeads := []fileHead{
		{line: 0, path: "add.txt", note: "new", add: 1, counted: true},
		{line: 4, path: "del.txt", note: "deleted", del: 1, counted: true},
		{line: 8, path: "img.bin", note: "binary", counted: true},
		{line: 10, path: "mod.sql", add: 1, del: 1, counted: true},
		{line: 17, path: "new.txt", orig: "old.txt", add: 1, counted: true},
		{line: 24, path: "run.sh", note: "mode changed", counted: true},
	}
	if !reflect.DeepEqual(heads, wantHeads) {
		t.Errorf("heads\n got: %+v\nwant: %+v", heads, wantHeads)
	}

	// A cut-off patch doesn't claim counts for its last file.
	_, heads = tidyPatch(splitDiff(patchFixture), true)
	if heads[len(heads)-1].counted || !heads[0].counted {
		t.Error("only the last file of a truncated patch should be uncounted")
	}
}

func TestTidyCombinedPatch(t *testing.T) {
	// Captured from `git show` of a merge whose conflict was resolved.
	in := "diff --cc f\nindex ba2906d,e45c9c2..49f33a8\n--- a/f\n+++ b/f\n@@@ -1,1 -1,1 +1,1 @@@\n- main\n -other\n++both\n"
	lines, heads := tidyPatch(splitDiff(in), false)
	if want := []string{"f", "@@@ -1,1 -1,1 +1,1 @@@", "- main", " -other", "++both"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines = %q, want %q", lines, want)
	}
	if want := []fileHead{{path: "f", add: 1, del: 2, counted: true}}; !reflect.DeepEqual(heads, want) {
		t.Errorf("heads = %+v, want %+v", heads, want)
	}
}

func TestTidyPatchQuotedPath(t *testing.T) {
	// git quotes names with special characters even with core.quotepath=false.
	in := "diff --git \"a/t\\tab\" \"b/t\\tab\"\nnew file mode 100644\nindex 0000000..45b983b\n--- /dev/null\n+++ \"b/t\\tab\"\n@@ -0,0 +1 @@\n+hi\n"
	if _, heads := tidyPatch(splitDiff(in), false); len(heads) != 1 || heads[0].path != "t\tab" {
		t.Errorf("heads = %+v, want the unquoted path", heads)
	}
}

func TestFileHeadView(t *testing.T) {
	withColors(t)
	st := theme.Default()
	h := fileHead{path: "src/new.go", orig: "src/old.go", note: "new", add: 20, del: 3, counted: true}
	got := h.view(st)
	if plain := ansi.Strip(got); plain != "src/old.go → src/new.go  new  +20  -3" {
		t.Errorf("file head = %q", plain)
	}
	if !strings.Contains(got, st.Muted.Render("src/")+st.DiffFile.Render("new.go")) {
		t.Errorf("directory not muted or name not emphasized: %q", got)
	}
	h.counted = false
	if plain := ansi.Strip(h.view(st)); strings.Contains(plain, "+20") {
		t.Errorf("uncounted file head shows counts: %q", plain)
	}
}

func TestRemovedDashLineIsRed(t *testing.T) {
	withColors(t)
	st := theme.Default()
	// A removed "-- comment" line is "--- comment" in the patch.
	if got := styleDiffLine(st, "--- comment"); got != st.DiffDel.Render("--- comment") {
		t.Errorf("removed line styled as %q", got)
	}
}

func TestFileListMutesDirectories(t *testing.T) {
	withColors(t)
	st := theme.Default()
	dir, text := paintOf(st.Muted), paintOf(st.Text)
	if got, want := pathView(dir, text, "src/pkg/a.go"), st.Muted.Render("src/pkg/")+st.Text.Render("a.go"); got != want {
		t.Errorf("pathView = %q, want %q", got, want)
	}
	if got, want := pathView(dir, text, "a.go"), st.Text.Render("a.go"); got != want {
		t.Errorf("pathView without a directory = %q, want %q", got, want)
	}
}
