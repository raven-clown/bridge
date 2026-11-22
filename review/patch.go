// Package review turns pull request patches into numbered lines, keeps
// review drafts and submits them.
package review

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/raven-clown/bridge/forge"
)

type LineKind int

const (
	Context LineKind = iota
	Added
	Removed
	HunkHeader
)

// Line.Old is 0 for added lines and New is 0 for removed lines; both are 0
// for hunk headers, whose Text is the section after the second @@.
type Line struct {
	Kind LineKind
	Old  int
	New  int
	Text string
}

type File struct {
	Patch     forge.FilePatch
	Lines     []Line
	Hunks     []int
	Missing   bool
	Collapsed bool
	Reason    string
}

const reasonMissing = "binary or too large to show"

var hunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

func Parse(p forge.FilePatch) (File, error) {
	f := File{Patch: p, Missing: p.PatchMissing}
	if f.Missing || p.Patch == "" {
		return f, nil
	}
	var oldN, newN int
	inHunk := false
	for _, raw := range strings.Split(strings.ReplaceAll(p.Patch, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(raw, "@@") {
			m := hunkHeader.FindStringSubmatch(raw)
			if m == nil {
				return File{}, fmt.Errorf("%s: cannot read hunk header %q", p.Path, raw)
			}
			oldN, _ = strconv.Atoi(m[1])
			newN, _ = strconv.Atoi(m[3])
			f.Hunks = append(f.Hunks, len(f.Lines))
			f.Lines = append(f.Lines, Line{Kind: HunkHeader, Text: m[5]})
			inHunk = true
			continue
		}
		if !inHunk || strings.HasPrefix(raw, `\`) {
			continue
		}
		switch {
		case strings.HasPrefix(raw, "+"):
			f.Lines = append(f.Lines, Line{Kind: Added, New: newN, Text: raw[1:]})
			newN++
		case strings.HasPrefix(raw, "-"):
			f.Lines = append(f.Lines, Line{Kind: Removed, Old: oldN, Text: raw[1:]})
			oldN++
		default:
			f.Lines = append(f.Lines, Line{Kind: Context, Old: oldN, New: newN, Text: strings.TrimPrefix(raw, " ")})
			oldN++
			newN++
		}
	}
	return f, nil
}

// Position is where a review comment on line i goes: added and context
// lines on the new side, removed lines on the old side.
func (f File) Position(i int) (int, forge.Side, bool) {
	if f.Missing || i < 0 || i >= len(f.Lines) {
		return 0, "", false
	}
	switch l := f.Lines[i]; l.Kind {
	case Added, Context:
		return l.New, forge.SideRight, true
	case Removed:
		return l.Old, forge.SideLeft, true
	}
	return 0, "", false
}
