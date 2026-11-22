package review

import (
	"path"
	"strings"
)

var lockFiles = map[string]bool{
	"go.sum": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "Cargo.lock": true,
	"poetry.lock": true, "composer.lock": true, "Gemfile.lock": true, "uv.lock": true,
}

var generatedSuffixes = []string{".min.js", ".pb.go", "_gen.go", ".gen.go"}

func generatedPath(p string) bool {
	for _, dir := range []string{"vendor/", "dist/"} {
		if strings.HasPrefix(p, dir) || strings.Contains(p, "/"+dir) {
			return true
		}
	}
	for _, s := range generatedSuffixes {
		if strings.HasSuffix(p, s) {
			return true
		}
	}
	return false
}

// generatedMarker follows Go's convention: a "Code generated ... DO NOT
// EDIT." line, here the first added line of the file.
func generatedMarker(f File) bool {
	for _, l := range f.Lines {
		if l.Kind == Added {
			return strings.Contains(l.Text, "Code generated") && strings.Contains(l.Text, "DO NOT EDIT")
		}
	}
	return false
}

// Collapse starts lock files, generated files and files GitHub left out
// folded, with the reason shown in their place.
func Collapse(f *File) {
	p := f.Patch.Path
	switch {
	case f.Missing:
		f.Collapsed, f.Reason = true, reasonMissing
	case lockFiles[path.Base(p)]:
		f.Collapsed, f.Reason = true, "lock file"
	case generatedPath(p) || generatedMarker(*f):
		f.Collapsed, f.Reason = true, "generated"
	}
}
