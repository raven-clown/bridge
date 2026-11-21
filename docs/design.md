# bridge: design

A terminal control room for a developer who maintains many repositories and whose coding agents push work to them. One screen shows everything that waits for you across every repository: pull requests to review, issues to triage, comments to answer, fork runs to approve. From that screen you read the thread, review the diff, reply, label, assign, add to a project, merge and release, with the same policy everywhere and without opening a browser tab per item. It runs on Windows Terminal with no tmux, no `gh` and no shell plugins.

Working name: `bridge`, as in a ship's bridge. The name is not final.

## Intent

**Who it is for.** A developer who is the only human behind several repositories, runs coding agents in parallel on branches and worktrees, receives outside pull requests and issues, and opens pull requests to other people's repositories. Reviewing, answering, triaging, merging and releasing that stream is their daily loop.

**What hurts today.** Every item is a browser tab. Read a comment here, open an issue there, add a label, set a milestone, assign someone, put it on a project board, open the diff, check CI, merge with the right flags, delete the branch, tag a release, write the notes, then the next repository. Each step has a good terminal tool for one repository and one object, but nothing stitches the loop across repositories, the write actions (labels, milestones, assignees, projects, replies) mostly still need the browser, and most agent-oriented terminal tools need tmux, which rules out Windows.

**Success looks like.** Opening one program in the morning, seeing a ranked list of everything that needs a human across every repository, and clearing it from that list: answer six comments, label and assign four issues, review and merge five pull requests in three repositories, cut one release, in a few minutes and without a browser. A user on Windows gets the same experience as a user on macOS or Linux.

**Constraints the author set.**
- One static binary. No runtime dependency on `gh`, `git`, tmux or a shell plugin. Git operations go through a library.
- Windows is a first-class target, tested in CI alongside Linux and macOS.
- GitHub first. Other forges come later behind the same interfaces.
- Everything is free, Apache-2.0, no hosted service and no telemetry.
- Go. The program is many concurrent API calls feeding one screen, which goroutines and channels express directly, and the author already ships Go daily. Rust was considered for the same design and stays an option for a later, smaller tool.

## Scope

**v0.1, the inbox, the thread and the review**
- Discover repositories under configured roots and map them to GitHub repositories by their remotes. Watch extra repositories named in config.
- Build one inbox across all of them: pull requests you opened, pull requests and issues opened against your repositories, review requests, comments that mention you or reply to you and have no answer from you yet, and branches pushed since you last looked that have no pull request.
- Show CI state per pull request, including fork workflow runs waiting for approval.
- Rank items by urgency.
- Open an item into its thread: body and every comment, review comments grouped by file, with reply and new comment in place.
- Triage from the inbox or the thread: labels, milestone, assignees, add to or move on a GitHub Project, close, reopen. Multi-select applies the same change to several items.
- Create an issue in any watched repository from a short form.
- Open a pull request into a continuous diff with file and hunk navigation and automatic collapsing of lock files and generated files.
- Add line comments, range comments and a summary. Drafts are saved locally until submitted.
- Submit a review: comment, approve or request changes.
- Set labels, milestone and assignees on a pull request you open, from a per-owner default, so sidebar metadata is never forgotten.

**v0.2, shipping**
- Merge with a per-repository or per-owner policy: method, commit title template, commit body template, delete branch after merge.
- Approve a fork's workflow runs, rerun failed jobs, update a branch from its base.
- After a merge, if the repository is checked out locally and clean, switch it to the default branch and fast-forward.
- Release: tag the merge commit, generate notes from the pull requests merged since the last tag, grouped by Conventional Commits type into configurable sections, and create the GitHub release.
- Project board view: the items of one GitHub Project as columns, move between columns, set fields.

**v0.3, repositories and agents**
- A repositories view with dirty, ahead, behind and stale worktrees per repository.
- Detection of coding agent processes running in a repository, so the inbox can say "an agent is still working here".
- Saved searches as extra inbox sections.

**Not in scope**
- Starting, stopping or talking to agent sessions. Other tools do this well, and a terminal multiplexer is the wrong dependency for Windows.
- Discussions, wikis, releases editing after publish, repository settings.
- A web UI, a daemon or a mobile view.
- GitLab, Gitea, Forgejo and Bitbucket in v0.x. The forge interface is designed so they can be added without touching the inbox, thread or review code.

## Approaches considered

1. **One TUI with a headless core (chosen).** Library packages hold the domain and the GitHub client. A thin command offers the TUI and the same operations as subcommands with JSON output. One program, one mental model, and the headless commands make the core testable and scriptable.
2. **Several small CLIs piped together** (`inbox`, `triage`, `review`, `ship`). Faster to start, but the value is the single room where the list, the thread, the diff and the action live together. Composition would push state through files and lose that.
3. **Contribute to gh-dash and tuicr.** The quickest path to a working loop, but it would be two programs with two configs, both depending on `gh`, neither with triage or projects, and neither owned by the author.

## Architecture

```
+----------------------------------------------------------------+
| cmd/bridge                                                      |
|   main.go       no args: TUI; subcommands: inbox, thread, ...   |
+----------------------------------------------------------------+
| internal/tui    bubbletea app: inbox, thread, review, ship,     |
|                 project views, triage panel                     |
+----------------------------------------------------------------+
                               |
+----------------------------------------------------------------+
| library packages (no terminal code)                             |
|   discover/   roots -> local repos -> forge repos               |
|   forge/      Forge interface; forge/github implements it      |
|   inbox/      items, ranking, refresh                          |
|   thread/     conversation model, replies                      |
|   triage/     labels, milestones, assignees, projects, state   |
|   review/     diff model, comments, drafts, submit             |
|   ship/       merge policy, post-merge, release notes          |
|   config/     YAML config, env overrides, validation           |
|   store/      drafts, seen marks and cache on disk             |
|   auth/       token sources: env, keyring, device flow         |
+----------------------------------------------------------------+
            |                      |                     |
      go-git (git)    go-github + githubv4 (API)     go-keyring
```

**Packages**
- Library packages at the module root (`discover`, `forge`, `inbox`, `thread`, `triage`, `review`, `ship`, `config`, `store`, `auth`): everything that is not a screen. No terminal code, no `fmt.Print`. Every public operation takes a `forge.Forge` and returns typed results, so tests run against a fake forge or recorded HTTP.
- `internal/tui`: the bubbletea program, one model per view, shared styles with lipgloss.
- `cmd/bridge`: the command. `bridge` with no arguments opens the TUI. Subcommands run one operation and print a table or `--json`: `bridge inbox`, `bridge thread <url>`, `bridge triage <url> --label x --milestone y --assign z --project p`, `bridge review <url>`, `bridge ship <url>`, `bridge release owner/name`, `bridge issue new owner/name`.

**Key dependencies**
- `charmbracelet/bubbletea`, `bubbles` and `lipgloss` for the terminal. Windows Terminal and ConPTY are supported.
- `google/go-github` for REST and `shurcooL/githubv4` for GraphQL, with typed query structs. Projects v2 is GraphQL only, so the GraphQL path is first class. Device flow login is a small package of our own over the two OAuth endpoints.
- `go-git/go-git` for local repositories: remotes, HEAD, ahead/behind, worktrees, dirty state, fast-forward. Pure Go, no `git` binary. Status on a large repository is the slow path, so discovery caches it and the repos view refreshes it in the background.
- `zalando/go-keyring` for the OS credential store (Windows Credential Manager, macOS Keychain, Secret Service).
- `gopkg.in/yaml.v3` for config, `spf13/cobra` for subcommands, `charmbracelet/x/exp/teatest` for TUI tests, `net/http/httptest` with recorded fixtures for API tests.

### Forge interface

```go
type Forge interface {
    WhoAmI(ctx context.Context) (User, error)

    // inbox
    ListMyPRs(ctx context.Context, since *time.Time) ([]PullRequest, error)
    ListIncoming(ctx context.Context, repos []RepoID, since *time.Time) ([]Item, error) // PRs and issues
    ListReviewRequests(ctx context.Context) ([]PullRequest, error)
    ListMentions(ctx context.Context, since *time.Time) ([]CommentRef, error)
    ListBranches(ctx context.Context, repo RepoID) ([]Branch, error)
    PRChecks(ctx context.Context, pr PRID) (Checks, error)

    // thread
    Thread(ctx context.Context, item ItemID) (Thread, error)
    Comment(ctx context.Context, item ItemID, body string, replyTo *CommentID) (CommentID, error)

    // triage
    RepoMeta(ctx context.Context, repo RepoID) (RepoMeta, error) // labels, milestones, collaborators, projects
    SetLabels(ctx context.Context, item ItemID, add, remove []string) error
    SetMilestone(ctx context.Context, item ItemID, milestone *MilestoneID) error
    SetAssignees(ctx context.Context, item ItemID, add, remove []string) error
    SetState(ctx context.Context, item ItemID, state ItemState) error
    CreateIssue(ctx context.Context, repo RepoID, issue NewIssue) (ItemID, error)
    ListProjects(ctx context.Context, owner string) ([]Project, error)
    ProjectItems(ctx context.Context, project ProjectID) ([]ProjectItem, error)
    AddToProject(ctx context.Context, project ProjectID, item ItemID) (ProjectItemID, error)
    SetProjectField(ctx context.Context, project ProjectID, item ProjectItemID, field FieldID, value FieldValue) error

    // review
    PRFiles(ctx context.Context, pr PRID) ([]FilePatch, error)
    SubmitReview(ctx context.Context, pr PRID, review Review) error

    // ship
    Merge(ctx context.Context, pr PRID, how MergeRequest) (MergeResult, error)
    DeleteBranch(ctx context.Context, repo RepoID, name string) error
    ApproveWorkflowRuns(ctx context.Context, pr PRID) (int, error)
    RerunFailed(ctx context.Context, pr PRID) (int, error)
    UpdateBranch(ctx context.Context, pr PRID) error
    MergedPRsBetween(ctx context.Context, repo RepoID, from, to SHA) ([]PullRequest, error)
    CreateRelease(ctx context.Context, repo RepoID, release ReleaseRequest) (string, error)
}
```

The GitHub implementation uses GraphQL for the list calls, the thread and everything about Projects, since one query returns an item with its labels, milestone, assignees, checks, review state and project fields in a page. Writes use REST where it exists and GraphQL for Projects. Rate limit headers are read on every response and shown in the status bar. The `read:project` scope is needed for Projects, and the program says so when it is missing instead of showing an empty list.

## Data model

```go
type LocalRepo struct { Path string; Remotes []Remote; Head Head; Dirty bool; Ahead, Behind int; Worktrees []Worktree }

type RepoID struct { Host, Owner, Name string }

type InboxKind int // OwnPR, IncomingPR, IncomingIssue, ReviewRequest, Mention, PushedBranch
// OwnPR          you opened it, anywhere
// IncomingPR     someone opened it against a repo you watch
// IncomingIssue  someone opened an issue against a repo you watch
// ReviewRequest  your review was requested
// Mention        a comment mentions you or replies to you, and you have not answered since
// PushedBranch   a branch moved and has no pull request yet

type InboxItem struct {
    Kind       InboxKind
    Repo       RepoID
    ID         ItemID        // issue or PR number; branch name for PushedBranch
    Title      string
    Author     User
    Labels     []Label
    Milestone  *Milestone
    Assignees  []User
    Projects   []ProjectRef
    Checks     *Checks       // PRs only: passing, failing, pending, awaiting_approval, none
    Review     *ReviewState  // PRs only: none, commented, approved, changes_requested
    Mergeable  *Mergeable    // PRs only: clean, conflicting, blocked, unknown
    Unanswered int           // comments after your last comment
    UpdatedAt  time.Time
    SeenAt     *time.Time    // from the local store
    Urgency    Urgency       // computed, see ranking
    Local      string        // where it is checked out, if anywhere
}

type Thread struct { Item ItemID; Body string; Comments []Comment; ReviewThreads []ReviewThread; Events []Event }

type RepoMeta struct { Labels []Label; Milestones []Milestone; Assignable []User; Projects []Project }

type Draft struct { PR PRID; HeadSHA SHA; Comments []DraftComment; Summary string; Viewed map[string]bool }

type SidebarDefaults struct { Labels []string; Milestone MilestoneRule; Assignee Assignee; Project string }

type MergePolicy struct { Method MergeMethod; Title, Body Template; DeleteBranch, RequireGreen bool } // Squash, Merge, Rebase

type ReleasePolicy struct { Tag Template; Sections []Section; Bump Bump } // Minor, Patch, Prompt
```

Templates use `{title}`, `{number}`, `{author}`, `{branch}`, `{repo}`, `{version}`. The default merge title is `{title} (#{number})` and the default body is empty, which keeps a co-author trailer off a squash commit when the local git identity differs from the account.

`MilestoneRule` is `none`, `current` (the open milestone with the nearest due date) or a name. `Assignee` is `none`, `me` or a login.

## Ranking

The inbox is one list sorted by urgency, then by age. Urgency is a small integer from rules, highest first:

1. Incoming pull request with a fork workflow run awaiting approval. Nothing happens until you click.
2. Mention or reply with no answer from you. A person is waiting.
3. Your pull request with a failed required check.
4. Review requested from you.
5. Incoming issue with no label and no assignee. Untriaged.
6. Incoming pull request with green checks and no review from you.
7. Your pull request with green checks and an approval, ready to merge.
8. Pushed branch with no pull request, agent likely finished.
9. Your pull request waiting on someone else, sorted by days waiting.
10. Everything else.

Rules live in one file with a table-driven test per rule. The user can mute a repository, an author or a kind.

## Data flow

1. **Discover.** Walk each root to a configured depth, open every `.git` with go-git, read remotes, map `github.com/owner/name` to a `RepoID`. Add the extra repositories from config. Cached on disk; a manual refresh rescans.
2. **Refresh.** In parallel: `list_my_prs`, `list_incoming` for the watched repositories, `list_review_requests`, `list_mentions`, and `list_branches` for repositories that are local. Results merge into `InboxItem`s, ranking runs, the view updates. `repo_meta` is fetched lazily per repository and cached for the session. Refresh runs on start, on demand and on a timer (default 90 seconds). Conditional requests keep a quiet refresh inside the rate limit.
3. **Thread.** Opening an item fetches `thread`. Replying calls `comment` and marks the item answered locally so it drops in the ranking at once, before the next refresh confirms it.
4. **Triage.** The triage panel reads `repo_meta` for pickers. Each change is one call. With several items selected, the same change runs per item and the panel reports each result. Project moves go through `add_to_project` and `set_project_field`.
5. **Review.** Opening a pull request fetches `pr_files`. Patches are parsed into a continuous diff with file headers. Lock files and generated files collapse by default. Comments go into a `Draft` keyed by pull request and head SHA; a new head SHA marks the draft stale and asks before reuse. Submit calls `submit_review` once with every comment, then clears the draft.
6. **Ship.** `merge` checks the policy (`require_green`, mergeable) and shows a confirmation with the exact title and method, then merges. On success: optional `delete_branch`, local post-merge if the repository is checked out and clean, then a refresh.
7. **Release.** Pick a repository, pick the bump, preview the tag and the notes from `merged_prs_between(last_tag, default_branch_head)`, then `create_release`. Notes follow the configured sections. The default sections are Added, Fixed and Changed, filled from `feat`, `fix` and everything else, each line `<title> by @<author> in #<number>`, followed by New Contributors and a Full Changelog link.

## Configuration

Path: the platform config directory, `%APPDATA%\bridge\config.yaml` on Windows, `~/.config/bridge/config.yaml` elsewhere, or `--config`. Any key can be overridden by `BRIDGE_<KEY>` with `__` between nested keys.

```yaml
roots: ["D:/workspace"]
depth: 2
refresh_seconds: 90

auth:
  source: keyring            # keyring | env | device_flow

repos:                       # repositories to watch that are not checked out
  - owner/name

policy:
  default:
    merge:
      method: squash
      title: "{title} (#{number})"
      body: ""
      delete_branch: true
      require_green: true
    release:
      tag: "v{version}"
      bump: prompt
    sidebar:
      labels: []
      milestone: current
      assignee: me
      project: ""
  "owner/*":                 # glob on owner/name, most specific wins
    merge:
      method: rebase
    sidebar:
      labels: [needs-review]

mute:
  repos: ["owner/archive-*"]
  authors: ["dependabot[bot]"]
  kinds: []
```

Token sources, in order: `BRIDGE_TOKEN` or `GITHUB_TOKEN` in the environment, then the OS keyring, then a device flow login that stores the token in the keyring. The token is never written to the config file or the log. Scopes needed: `repo`, `workflow`, `read:org`, `read:project` and `project` for board writes.

## Local state

`%LOCALAPPDATA%\bridge\` on Windows, `~/.local/state/bridge/` elsewhere:
- `cache/` discovered repositories, repository metadata and the last inbox, so the program draws at once and refreshes behind.
- `drafts/` one JSON file per pull request with pending review comments, and one per thread with an unsent reply.
- `seen.json` the last time each item was looked at and answered, which drives the "new since" marker and the `unanswered` count between refreshes.

Everything in the state directory can be deleted without losing anything on GitHub.

## Terminal UI

Views on tabs, vim-style keys plus arrows, a status bar with account, rate limit, last refresh and the current error if any.

- **Inbox.** One row per item: urgency marker, repo, kind glyph, number, title, author, labels, milestone, checks glyph, review glyph, unanswered count, age. `enter` opens the thread, `d` opens the review for a pull request, `space` selects for multi-action, `t` triage panel, `c` quick comment, `m` merge, `a` approve fork runs, `r` rerun failed, `u` update branch, `n` new issue, `o` open in browser, `x` mute, `/` filter, `R` refresh. Section headers group by kind; `tab` jumps between sections.
- **Thread.** Body, comments and review threads in order, with author, time and reactions. `r` reply to the comment under the cursor, `c` new comment, `t` triage panel, `d` review (pull requests), `e` edit your own comment, `q` back. The compose box is multi-line Markdown with `ctrl+s` to send and `esc` to keep as a draft.
- **Triage panel.** A side panel over the inbox or thread with pickers for labels (multi-select with fuzzy search), milestone, assignees, project and project column, and state. Applies to the current item or every selected item. Shows one result line per item.
- **Review.** Left: file list with viewed marks. Right: continuous diff. `]` `[` next and previous hunk, `}` `{` next and previous file, `c` comment on line, `v` then `c` comment on range, `s` summary, `S` submit with a picker: comment, approve, request changes. `z` toggles collapsed files.
- **Ship.** A confirmation panel over the inbox: policy, resulting title, method, what happens after. `y` to go.
- **Release.** Repository picker, bump picker, preview of tag and notes, `y` to create.
- **Project** (v0.2). One GitHub Project as columns, items as cards with repo and number, `h` `l` move between columns, `enter` opens the thread, `f` edit a field.
- **Repos** (v0.3). One row per local repository with dirty, ahead, behind, worktrees, agent running.

Every action that changes GitHub (comment, triage change, merge, delete branch, release, submit review, close) shows what it will do and waits for `y`. A config flag turns confirmation off per action for people who trust their policy; comments and triage changes default to no confirmation since they are cheap to undo.

## Error handling

- The inbox is never silently incomplete. A repository or query that failed shows a row saying so, with the error, and a key to retry.
- Offline or rate limited: the cached inbox is shown with a banner and the time it was fetched.
- Drafts survive crashes: every review comment and unsent reply is written to disk when typed.
- A failed write (merge, label, project move) returns GitHub's message verbatim in the panel, and a multi-item triage keeps going and lists which items failed.
- Logs are JSON to a file in the state directory, with `--verbose` adding request summaries. Tokens and comment bodies never appear in logs.

## Testing

- Library unit tests for discovery (fixture repositories built with go-git in a temp dir), ranking (one table-driven test per rule), templates, sidebar defaults, release notes grouping, and config loading with env overrides.
- Forge tests run against `httptest.Server` with recorded GitHub REST and GraphQL responses checked into `testdata/`. No network in the test suite. The TUI and the subcommands are tested against a fake `Forge` in `forge/forgetest`.
- TUI tests use `teatest` and golden files for each view and for the key flows: open inbox, open thread, reply, triage two selected items, open review, add a comment, submit, confirm merge.
- CI: build and test with `-race` on `windows-latest`, `ubuntu-latest` and `macos-latest`, `golangci-lint`, coverage of 80% or more, `govulncheck`. Release workflow uses goreleaser to build binaries with checksums for Windows x64 and arm64, Linux x64 and arm64, macOS arm64, and publishes winget and Homebrew manifests.

## Security

- The token lives in the OS keyring or the environment, never on disk in plain text.
- No telemetry, no update check without an explicit `bridge update --check`.
- Review bodies, replies and diffs stay on the machine until the user sends them.
- The binary makes requests only to the configured forge host.

## Milestones

1. **v0.1** Discovery, auth, inbox with ranking, thread with reply, triage panel (labels, milestone, assignees, projects, state), new issue, review with drafts and submit, sidebar defaults, `inbox`, `thread`, `triage`, `review` and `issue new` subcommands. Windows, Linux and macOS binaries.
2. **v0.2** Merge policy, approve fork runs, rerun failed, update branch, post-merge local sync, release with notes, project board view.
3. **v0.3** Repos view, agent process detection, saved searches, mute rules in the UI.
4. **Later** GitLab behind the `Forge` trait, GitHub Enterprise hosts, multiple accounts.

## Open questions

- Name. `bridge` collides with common words and several Go modules. Candidates: `conn` (a ship's conning station), `quarterdeck`, `helm` and `tower` are taken by well-known projects.
- Whether v0.1 should include `PushedBranch` items, which need `list_branches` per repository and cost one request each. Default is on for local repositories only.
- Whether `Mention` should also cover review comment replies on your own pull requests, which GitHub does not count as mentions. Default is yes, found by walking review threads on your open pull requests.
