// Package forgetest provides an in-memory Forge for tests.
package forgetest

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/raven-clown/bridge/forge"
)

type Fake struct {
	mu sync.Mutex

	Me             string
	MyPRs          []forge.Item
	Incoming       []forge.Item
	ReviewRequests []forge.Item
	Mentions       []forge.CommentRef
	MyPRsTrunc     bool
	Branches       map[forge.RepoID][]forge.Branch
	Checks         map[forge.ItemID]forge.Checks
	Threads        map[forge.ItemID]forge.Thread
	Meta           map[forge.RepoID]forge.RepoMeta
	Projects       map[string][]forge.Project
	ProjectItemsBy map[forge.ProjectID][]forge.ProjectItem
	States         map[forge.ItemID]forge.ItemState
	Err            map[string]error
	Calls          []string

	// Block, when set for a method name, is waited on before the method returns.
	Block map[string]chan struct{}
	// ReadOnly repositories are left out by Writable; all others are writable.
	ReadOnly map[forge.RepoID]bool
	Diffs    map[forge.PRID]forge.Diff
	Reviews  map[forge.PRID][]forge.Review

	seq          int
	lastIncoming []forge.RepoID
}

// LastIncoming is the repository list the last ListIncoming call received.
func (f *Fake) LastIncoming() []forge.RepoID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.lastIncoming)
}

func (f *Fake) Writable(_ context.Context, repos []forge.RepoID) ([]forge.RepoID, error) {
	if err := f.enter("Writable"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.RepoID
	for _, r := range repos {
		if !f.ReadOnly[r] {
			out = append(out, r)
		}
	}
	return out, nil
}

func New() *Fake {
	return &Fake{
		Me:             "me",
		Branches:       map[forge.RepoID][]forge.Branch{},
		Checks:         map[forge.ItemID]forge.Checks{},
		Threads:        map[forge.ItemID]forge.Thread{},
		Meta:           map[forge.RepoID]forge.RepoMeta{},
		Projects:       map[string][]forge.Project{},
		ProjectItemsBy: map[forge.ProjectID][]forge.ProjectItem{},
		States:         map[forge.ItemID]forge.ItemState{},
		Err:            map[string]error{},
		Block:          map[string]chan struct{}{},
		Diffs:          map[forge.PRID]forge.Diff{},
		Reviews:        map[forge.PRID][]forge.Review{},
	}
}

func (f *Fake) PRDiff(_ context.Context, pr forge.PRID) (forge.Diff, error) {
	if err := f.enter("PRDiff"); err != nil {
		return forge.Diff{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.Diffs[pr]
	if !ok {
		return forge.Diff{}, forge.ErrNotFound
	}
	d.Files = slices.Clone(d.Files)
	return d, nil
}

// SubmitReview refuses a review written against an older head, as GitHub
// does when commented lines no longer exist.
func (f *Fake) SubmitReview(_ context.Context, pr forge.PRID, rv forge.Review) error {
	if err := f.enter("SubmitReview"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.Diffs[pr]; ok && d.HeadSHA != rv.CommitSHA {
		return forge.ErrStaleHead
	}
	f.Reviews[pr] = append(f.Reviews[pr], rv)
	return nil
}

func (f *Fake) CallLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Calls)
}

func (f *Fake) enter(name string) error {
	f.mu.Lock()
	f.Calls = append(f.Calls, name)
	ch := f.Block[name]
	err := f.Err[name]
	f.mu.Unlock()
	if ch != nil {
		<-ch
	}
	return err
}

func (f *Fake) next(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s%d", prefix, f.seq)
}

func (f *Fake) update(id forge.ItemID, fn func(*forge.Item)) error {
	found := false
	for _, list := range [][]forge.Item{f.MyPRs, f.Incoming, f.ReviewRequests} {
		for i := range list {
			if list[i].ID == id {
				fn(&list[i])
				found = true
			}
		}
	}
	if th, ok := f.Threads[id]; ok {
		fn(&th.Item)
		f.Threads[id] = th
		found = true
	}
	if !found {
		return forge.ErrNotFound
	}
	return nil
}

func (f *Fake) WhoAmI(context.Context) (forge.User, error) {
	if err := f.enter("WhoAmI"); err != nil {
		return forge.User{}, err
	}
	return forge.User{Login: f.Me}, nil
}

func (f *Fake) ListMyPRs(context.Context, *time.Time) (forge.ListResult[forge.Item], error) {
	if err := f.enter("ListMyPRs"); err != nil {
		return forge.ListResult[forge.Item]{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return forge.ListResult[forge.Item]{Items: slices.Clone(f.MyPRs), Truncated: f.MyPRsTrunc}, nil
}

func (f *Fake) ListIncoming(_ context.Context, repos []forge.RepoID, _ *time.Time) (forge.ListResult[forge.Item], error) {
	if err := f.enter("ListIncoming"); err != nil {
		return forge.ListResult[forge.Item]{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastIncoming = slices.Clone(repos)
	var out []forge.Item
	for _, it := range f.Incoming {
		if slices.Contains(repos, it.ID.Repo) {
			out = append(out, it)
		}
	}
	return forge.ListResult[forge.Item]{Items: out}, nil
}

func (f *Fake) ListReviewRequests(context.Context) (forge.ListResult[forge.Item], error) {
	if err := f.enter("ListReviewRequests"); err != nil {
		return forge.ListResult[forge.Item]{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return forge.ListResult[forge.Item]{Items: slices.Clone(f.ReviewRequests)}, nil
}

func (f *Fake) ListMentions(context.Context, *time.Time) (forge.ListResult[forge.CommentRef], error) {
	if err := f.enter("ListMentions"); err != nil {
		return forge.ListResult[forge.CommentRef]{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return forge.ListResult[forge.CommentRef]{Items: slices.Clone(f.Mentions)}, nil
}

func (f *Fake) ListBranches(_ context.Context, repo forge.RepoID) ([]forge.Branch, error) {
	if err := f.enter("ListBranches"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Branches[repo]), nil
}

func (f *Fake) PRChecks(_ context.Context, pr forge.PRID) (forge.Checks, error) {
	if err := f.enter("PRChecks"); err != nil {
		return forge.Checks{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Checks[pr], nil
}

func (f *Fake) Thread(_ context.Context, item forge.ItemID) (forge.Thread, error) {
	if err := f.enter("Thread"); err != nil {
		return forge.Thread{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	th, ok := f.Threads[item]
	if !ok {
		return forge.Thread{}, forge.ErrNotFound
	}
	th.Comments = slices.Clone(th.Comments)
	return th, nil
}

func (f *Fake) Comment(_ context.Context, item forge.ItemID, body string, replyTo *forge.CommentID) (forge.CommentID, error) {
	if err := f.enter("Comment"); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	th, ok := f.Threads[item]
	if !ok {
		return "", forge.ErrNotFound
	}
	c := forge.Comment{ID: forge.CommentID(f.next("C")), Author: forge.User{Login: f.Me}, At: time.Now(), Body: body}
	if replyTo != nil {
		c.ReplyTo = *replyTo
	}
	th.Comments = append(th.Comments, c)
	th.Item.LastCommentBy = f.Me
	th.Item.LastCommentAt = c.At
	f.Threads[item] = th
	return c.ID, nil
}

func (f *Fake) RepoMeta(_ context.Context, repo forge.RepoID) (forge.RepoMeta, error) {
	if err := f.enter("RepoMeta"); err != nil {
		return forge.RepoMeta{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Meta[repo], nil
}

func (f *Fake) SetLabels(_ context.Context, item forge.ItemID, add, remove []string) error {
	if err := f.enter("SetLabels"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.update(item, func(it *forge.Item) {
		var out []forge.Label
		for _, l := range it.Labels {
			if !slices.Contains(remove, l.Name) && !slices.Contains(add, l.Name) {
				out = append(out, l)
			}
		}
		for _, a := range add {
			out = append(out, forge.Label{Name: a})
		}
		it.Labels = out
	})
}

func (f *Fake) SetMilestone(_ context.Context, item forge.ItemID, m *forge.MilestoneID) error {
	if err := f.enter("SetMilestone"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	var ms *forge.Milestone
	if m != nil {
		for _, meta := range f.Meta {
			for _, x := range meta.Milestones {
				if x.Number == int(*m) {
					ms = &x
				}
			}
		}
		if ms == nil {
			ms = &forge.Milestone{Number: int(*m)}
		}
	}
	return f.update(item, func(it *forge.Item) { it.Milestone = ms })
}

func (f *Fake) SetAssignees(_ context.Context, item forge.ItemID, add, remove []string) error {
	if err := f.enter("SetAssignees"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.update(item, func(it *forge.Item) {
		var out []forge.User
		for _, u := range it.Assignees {
			if !slices.Contains(remove, u.Login) && !slices.Contains(add, u.Login) {
				out = append(out, u)
			}
		}
		for _, a := range add {
			out = append(out, forge.User{Login: a})
		}
		it.Assignees = out
	})
}

// SetState records into States, since Item has no state field.
func (f *Fake) SetState(_ context.Context, item forge.ItemID, state forge.ItemState) error {
	if err := f.enter("SetState"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.update(item, func(*forge.Item) {}); err != nil {
		return err
	}
	f.States[item] = state
	return nil
}

func (f *Fake) CreateIssue(_ context.Context, repo forge.RepoID, issue forge.NewIssue) (forge.ItemID, error) {
	if err := f.enter("CreateIssue"); err != nil {
		return forge.ItemID{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := forge.ItemID{Repo: repo, Number: 1000 + f.seq}
	it := forge.Item{ID: id, Title: issue.Title, Author: forge.User{Login: f.Me}}
	for _, l := range issue.Labels {
		it.Labels = append(it.Labels, forge.Label{Name: l})
	}
	for _, a := range issue.Assignees {
		it.Assignees = append(it.Assignees, forge.User{Login: a})
	}
	if issue.Milestone != nil {
		it.Milestone = &forge.Milestone{Number: int(*issue.Milestone)}
	}
	f.Threads[id] = forge.Thread{Item: it, Body: issue.Body}
	return id, nil
}

func (f *Fake) ListProjects(_ context.Context, owner string) ([]forge.Project, error) {
	if err := f.enter("ListProjects"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.Projects[owner]), nil
}

func (f *Fake) ProjectItems(_ context.Context, p forge.ProjectID) ([]forge.ProjectItem, error) {
	if err := f.enter("ProjectItems"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ProjectItemsBy[p]), nil
}

func (f *Fake) AddToProject(_ context.Context, p forge.ProjectID, item forge.ItemID) (forge.ProjectItemID, error) {
	if err := f.enter("AddToProject"); err != nil {
		return "", err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, pi := range f.ProjectItemsBy[p] {
		if pi.Item == item {
			return pi.ID, nil
		}
	}
	id := forge.ProjectItemID(f.next("PI"))
	f.ProjectItemsBy[p] = append(f.ProjectItemsBy[p], forge.ProjectItem{ID: id, Item: item, Fields: map[string]string{}})
	return id, nil
}

func (f *Fake) SetProjectField(_ context.Context, p forge.ProjectID, item forge.ProjectItemID, field forge.FieldID, v forge.FieldValue) error {
	if err := f.enter("SetProjectField"); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	items := f.ProjectItemsBy[p]
	for i := range items {
		if items[i].ID == item {
			val := v.OptionID
			if val == "" {
				val = v.Text
			}
			items[i].Fields[string(field)] = val
			return nil
		}
	}
	return forge.ErrNotFound
}
