package review

import (
	"strings"
	"testing"

	"github.com/raven-clown/bridge/forge"
)

const twoHunks = `@@ -1,3 +1,4 @@ package main
 package main
-import "fmt"
+import (
+	"fmt"

@@ -10,2 +11,2 @@ func main() {
-	fmt.Println("hi")
+	fmt.Println("hello")
 }`

func TestParseNumbersLines(t *testing.T) {
	f, err := Parse(forge.FilePatch{Path: "main.go", Patch: twoHunks})
	if err != nil {
		t.Fatal(err)
	}
	type want struct {
		kind     LineKind
		old, new int
	}
	exp := []want{
		{HunkHeader, 0, 0},
		{Context, 1, 1},
		{Removed, 2, 0},
		{Added, 0, 2},
		{Added, 0, 3},
		{Context, 3, 4},
		{HunkHeader, 0, 0},
		{Removed, 10, 0},
		{Added, 0, 11},
		{Context, 11, 12},
	}
	if len(f.Lines) != len(exp) {
		t.Fatalf("%d lines, want %d: %+v", len(f.Lines), len(exp), f.Lines)
	}
	for i, w := range exp {
		l := f.Lines[i]
		if l.Kind != w.kind || l.Old != w.old || l.New != w.new {
			t.Errorf("line %d = %+v, want %+v", i, l, w)
		}
	}
	if len(f.Hunks) != 2 || f.Hunks[0] != 0 || f.Hunks[1] != 6 {
		t.Fatalf("hunks = %v", f.Hunks)
	}
	if f.Lines[3].Text != "import (" {
		t.Fatalf("text = %q", f.Lines[3].Text)
	}
}

func TestParseOmittedCounts(t *testing.T) {
	f, err := Parse(forge.FilePatch{Path: "a", Patch: "@@ -1 +1 @@\n-a\n+b"})
	if err != nil || f.Lines[1].Old != 1 || f.Lines[2].New != 1 {
		t.Fatalf("lines %+v err %v", f.Lines, err)
	}
}

func TestParseNoNewlineMarker(t *testing.T) {
	f, err := Parse(forge.FilePatch{Path: "a", Patch: "@@ -1 +1,2 @@\n-a\n\\ No newline at end of file\n+a\n+b"})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range f.Lines {
		if strings.HasPrefix(l.Text, "No newline") || strings.HasPrefix(l.Text, "\\") {
			t.Fatalf("marker kept as a line: %+v", f.Lines)
		}
	}
	if last := f.Lines[len(f.Lines)-1]; last.New != 2 {
		t.Fatalf("last = %+v", last)
	}
}

func TestParseTrailingNewlineAddsNoLine(t *testing.T) {
	for _, patch := range []string{"@@ -1 +1 @@\n-a\n+b\n", "@@ -1 +1 @@\r\n-a\r\n+b\r\n"} {
		f, err := Parse(forge.FilePatch{Path: "a", Patch: patch})
		if err != nil || len(f.Lines) != 3 {
			t.Fatalf("%q: lines %+v err %v", patch, f.Lines, err)
		}
	}
}

func TestParseMalformedHeader(t *testing.T) {
	_, err := Parse(forge.FilePatch{Path: "broken.go", Patch: "@@ nope @@\n+x"})
	if err == nil || !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("got %v", err)
	}
}

func TestParseMissingPatch(t *testing.T) {
	for _, p := range []forge.FilePatch{
		{Path: "big.go", Additions: 3, PatchMissing: true},
		{Path: "img.png", PatchMissing: true},
	} {
		f, err := Parse(p)
		if err != nil {
			t.Fatal(err)
		}
		Collapse(&f)
		if !f.Missing || !f.Collapsed || f.Reason != "binary or too large to show" || len(f.Lines) != 0 {
			t.Errorf("%s: %+v", p.Path, f)
		}
	}
}

func TestEmptyPatchIsNotMissing(t *testing.T) {
	f, err := Parse(forge.FilePatch{Path: "empty.txt", Status: forge.FileAdded})
	if err != nil || f.Missing {
		t.Fatalf("file %+v err %v", f, err)
	}
}

func TestPosition(t *testing.T) {
	f, err := Parse(forge.FilePatch{Path: "main.go", Patch: twoHunks})
	if err != nil {
		t.Fatal(err)
	}
	if line, side, ok := f.Position(3); !ok || line != 2 || side != forge.SideRight {
		t.Errorf("added: %d %s %v", line, side, ok)
	}
	if line, side, ok := f.Position(2); !ok || line != 2 || side != forge.SideLeft {
		t.Errorf("removed: %d %s %v", line, side, ok)
	}
	if line, side, ok := f.Position(1); !ok || line != 1 || side != forge.SideRight {
		t.Errorf("context: %d %s %v", line, side, ok)
	}
	if _, _, ok := f.Position(0); ok {
		t.Error("hunk header is not a comment target")
	}
	if _, _, ok := f.Position(99); ok {
		t.Error("out of range is not a comment target")
	}
}
