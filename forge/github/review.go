package github

import (
	"context"
	"fmt"
	"net/http"

	"github.com/raven-clown/bridge/forge"
)

const (
	filesPerPage = 100
	maxFilePages = 30 // GitHub lists at most 3000 files
)

func pullPath(pr forge.PRID) string { return fmt.Sprintf("%s/pulls/%d", repoPath(pr.Repo), pr.Number) }

type ghFile struct {
	Filename         string  `json:"filename"`
	PreviousFilename string  `json:"previous_filename"`
	Status           string  `json:"status"`
	Additions        int     `json:"additions"`
	Deletions        int     `json:"deletions"`
	Patch            *string `json:"patch"`
}

func (g ghFile) toForge() forge.FilePatch {
	fp := forge.FilePatch{Path: g.Filename, PreviousPath: g.PreviousFilename, Additions: g.Additions, Deletions: g.Deletions}
	switch g.Status {
	case "added":
		fp.Status = forge.FileAdded
	case "removed":
		fp.Status = forge.FileRemoved
	case "renamed":
		fp.Status = forge.FileRenamed
	default:
		fp.Status = forge.FileModified
	}
	if g.Patch == nil {
		fp.PatchMissing = true
	} else {
		fp.Patch = *g.Patch
	}
	return fp
}

func (c *Client) PRDiff(ctx context.Context, pr forge.PRID) (forge.Diff, error) {
	var pull struct {
		Head struct {
			SHA string `json:"sha"`
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	}
	if err := c.rest(ctx, http.MethodGet, pullPath(pr), nil, &pull); err != nil {
		return forge.Diff{}, err
	}
	d := forge.Diff{PR: pr, HeadSHA: forge.SHA(pull.Head.SHA), HeadRef: pull.Head.Ref, BaseRef: pull.Base.Ref}
	for page := 1; page <= maxFilePages; page++ {
		var batch []ghFile
		if err := c.rest(ctx, http.MethodGet, fmt.Sprintf("%s/files?per_page=%d&page=%d", pullPath(pr), filesPerPage, page), nil, &batch); err != nil {
			return forge.Diff{}, err
		}
		for _, f := range batch {
			d.Files = append(d.Files, f.toForge())
		}
		if len(batch) < filesPerPage {
			return d, nil
		}
	}
	d.Truncated = true
	return d, nil
}

func (c *Client) SubmitReview(ctx context.Context, pr forge.PRID, rv forge.Review) error {
	comments := []map[string]any{}
	for _, cm := range rv.Comments {
		m := map[string]any{"path": cm.Path, "line": cm.Line, "side": string(cm.Side), "body": cm.Body}
		if cm.StartLine > 0 {
			m["start_line"], m["start_side"] = cm.StartLine, string(cm.StartSide)
		}
		comments = append(comments, m)
	}
	body := map[string]any{"commit_id": string(rv.CommitSHA), "event": string(rv.Event), "body": rv.Body, "comments": comments}
	return c.rest(ctx, http.MethodPost, pullPath(pr)+"/reviews", body, nil)
}
