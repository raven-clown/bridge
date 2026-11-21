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
- Rust.

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

1. **One TUI with a headless core (chosen).** A library crate holds the domain and the GitHub client. A thin binary offers the TUI and the same operations as subcommands with JSON output. One program, one mental model, and the headless commands make the core testable and scriptable.
2. **Several small CLIs piped together** (`inbox`, `triage`, `review`, `ship`). Faster to start, but the value is the single room where the list, the thread, the diff and the action live together. Composition would push state through files and lose that.
3. **Contribute to gh-dash and tuicr.** The quickest path to a working loop, but it would be two programs with two configs, both depending on `gh`, neither with triage or projects, and neither owned by the author.

## Architecture

```
+----------------------------------------------------------------+
| bridge (binary)                                                 |
|   tui/     ratatui app: inbox, thread, review, ship, project    |
|   cli/     subcommands: inbox, thread, triage, review, ship ... |
+----------------------------------------------------------------+
                               |
+----------------------------------------------------------------+
| bridge-core (library)                                           |
|   discover/   roots -> local repos -> forge repos               |
|   forge/      Forge trait; forge/github implements it          |
|   inbox/      items, ranking, refresh                          |
|   thread/     conversation model, replies                      |
|   triage/     labels, milestones, assignees, projects, state   |
|   review/     diff model, comments, drafts, submit             |
|   ship/       merge policy, post-merge, release notes          |
|   config/     TOML config, env overrides, validation           |
|   store/      drafts, seen marks and cache on disk             |
|   auth/       token sources: env, keyring, device flow         |
+----------------------------------------------------------------+
            |                      |                     |
         gix (git)        octocrab (GitHub API)      keyring
```

**Crates**
- `bridge-core`: everything that is not a screen. No terminal code, no `println!`. Every public operation takes a `Forge` and returns typed results, so tests run against a mock forge or recorded HTTP.
- `bridge`: the binary. `bridge` with no arguments opens the TUI. Subcommands run one operation and print a table or `--json`: `bridge inbox`, `bridge thread <url>`, `bridge triage <url> --label x --milestone y --assign z --project p`, `bridge review <url>`, `bridge ship <url>`, `bridge release owner/name`, `bridge issue new owner/name`.

**Key dependencies**
- `ratatui` + `crossterm` for the terminal. crossterm has solid Windows support.
- `octocrab` for GitHub REST and GraphQL. Device flow login is built in. Projects v2 is GraphQL only, so the GraphQL path is first class.
- `gix` (gitoxide) for local repositories: remotes, HEAD, ahead/behind, worktrees, dirty state, fast-forward. Pure Rust, no `git` binary.
- `keyring` for the OS credential store (Windows Credential Manager, macOS Keychain, Secret Service).
- `tokio` for I/O; `serde` + `toml` for config; `insta` for snapshot tests; `wiremock` for recorded API tests.

### Forge interface

```rust
trait Forge {
    async fn whoami(&self) -> Result<User>;

    // inbox
    async fn list_my_prs(&self, since: Option<Timestamp>) -> Result<Vec<PullRequest>>;
    async fn list_incoming(&self, repos: &[RepoId], since: Option<Timestamp>) -> Result<Vec<Item>>; // PRs and issues
    async fn list_review_requests(&self) -> Result<Vec<PullRequest>>;
    async fn list_mentions(&self, since: Option<Timestamp>) -> Result<Vec<CommentRef>>;
    async fn list_branches(&self, repo: &RepoId) -> Result<Vec<Branch>>;
    async fn pr_checks(&self, pr: &PrId) -> Result<Checks>;

    // thread
    async fn thread(&self, item: &ItemId) -> Result<Thread>;
    async fn comment(&self, item: &ItemId, body: &str, reply_to: Option<CommentId>) -> Result<CommentId>;

    // triage
    async fn repo_meta(&self, repo: &RepoId) -> Result<RepoMeta>; // labels, milestones, collaborators, projects
    async fn set_labels(&self, item: &ItemId, add: &[String], remove: &[String]) -> Result<()>;
    async fn set_milestone(&self, item: &ItemId, milestone: Option<MilestoneId>) -> Result<()>;
    async fn set_assignees(&self, item: &ItemId, add: &[String], remove: &[String]) -> Result<()>;
    async fn set_state(&self, item: &ItemId, state: ItemState) -> Result<()>;
    async fn create_issue(&self, repo: &RepoId, issue: &NewIssue) -> Result<ItemId>;
    async fn list_projects(&self, owner: &str) -> Result<Vec<Project>>;
    async fn project_items(&self, project: &ProjectId) -> Result<Vec<ProjectItem>>;
    async fn add_to_project(&self, project: &ProjectId, item: &ItemId) -> Result<ProjectItemId>;
    async fn set_project_field(&self, project: &ProjectId, item: &ProjectItemId, field: &FieldId, value: FieldValue) -> Result<()>;

    // review
    async fn pr_files(&self, pr: &PrId) -> Result<Vec<FilePatch>>;
    async fn submit_review(&self, pr: &PrId, review: &Review) -> Result<()>;

    // ship
    async fn merge(&self, pr: &PrId, how: &MergeRequest) -> Result<MergeResult>;
    async fn delete_branch(&self, repo: &RepoId, name: &str) -> Result<()>;
    async fn approve_workflow_runs(&self, pr: &PrId) -> Result<u32>;
    async fn rerun_failed(&self, pr: &PrId) -> Result<u32>;
    async fn update_branch(&self, pr: &PrId) -> Result<()>;
    async fn merged_prs_between(&self, repo: &RepoId, from: &Sha, to: &Sha) -> Result<Vec<PullRequest>>;
    async fn create_release(&self, repo: &RepoId, release: &ReleaseRequest) -> Result<Url>;
}
```

The GitHub implementation uses GraphQL for the list calls, the thread and everything about Projects, since one query returns an item with its labels, milestone, assignees, checks, review state and project fields in a page. Writes use REST where it exists and GraphQL for Projects. Rate limit headers are read on every response and shown in the status bar. The `read:project` scope is needed for Projects, and the program says so when it is missing instead of showing an empty list.

## Data model

```rust
struct LocalRepo { path: PathBuf, remotes: Vec<Remote>, head: Head, dirty: bool, ahead: u32, behind: u32, worktrees: Vec<Worktree> }

struct RepoId { host: String, owner: String, name: String }

enum InboxKind {
    OwnPr,          // you opened it, anywhere
    IncomingPr,     // someone opened it against a repo you watch
    IncomingIssue,  // someone opened an issue against a repo you watch
    ReviewRequest,  // your review was requested
    Mention,        // a comment mentions you or replies to you, and you have not answered since
    PushedBranch,   // a branch moved and has no pull request yet
}

struct InboxItem {
    kind: InboxKind,
    repo: RepoId,
    id: ItemId,                     // issue or PR number; branch name for PushedBranch
    title: String,
    author: User,
    labels: Vec<Label>,
    milestone: Option<Milestone>,
    assignees: Vec<User>,
    projects: Vec<ProjectRef>,
    checks: Option<Checks>,         // PRs only: passing, failing, pending, awaiting_approval, none
    review: Option<ReviewState>,    // PRs only: none, commented, approved, changes_requested
    mergeable: Option<Mergeable>,   // PRs only: clean, conflicting, blocked, unknown
    unanswered: u32,                // comments after your last comment
    updated_at: Timestamp,
    seen_at: Option<Timestamp>,     // from the local store
    urgency: Urgency,               // computed, see ranking
    local: Option<PathBuf>,         // where it is checked out, if anywhere
}

struct Thread { item: ItemId, body: Markdown, comments: Vec<Comment>, review_threads: Vec<ReviewThread>, events: Vec<Event> }

struct RepoMeta { labels: Vec<Label>, milestones: Vec<Milestone>, assignable: Vec<User>, projects: Vec<Project> }

struct Draft { pr: PrId, head_sha: Sha, comments: Vec<DraftComment>, summary: String, viewed: BTreeSet<PathBuf> }

struct SidebarDefaults { labels: Vec<String>, milestone: MilestoneRule, assignee: Assignee, project: Option<String> }

struct MergePolicy { method: Squash | Merge | Rebase, title: Template, body: Template, delete_branch: bool, require_green: bool }

struct ReleasePolicy { tag: Template, sections: Vec<Section>, bump: Minor | Patch | Prompt }
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

1. **Discover.** Walk each root to a configured depth, open every `.git` with gix, read remotes, map `github.com/owner/name` to a `RepoId`. Add the extra repositories from config. Cached on disk; a manual refresh rescans.
2. **Refresh.** In parallel: `list_my_prs`, `list_incoming` for the watched repositories, `list_review_requests`, `list_mentions`, and `list_branches` for repositories that are local. Results merge into `InboxItem`s, ranking runs, the view updates. `repo_meta` is fetched lazily per repository and cached for the session. Refresh runs on start, on demand and on a timer (default 90 seconds). Conditional requests keep a quiet refresh inside the rate limit.
3. **Thread.** Opening an item fetches `thread`. Replying calls `comment` and marks the item answered locally so it drops in the ranking at once, before the next refresh confirms it.
4. **Triage.** The triage panel reads `repo_meta` for pickers. Each change is one call. With several items selected, the same change runs per item and the panel reports each result. Project moves go through `add_to_project` and `set_project_field`.
5. **Review.** Opening a pull request fetches `pr_files`. Patches are parsed into a continuous diff with file headers. Lock files and generated files collapse by default. Comments go into a `Draft` keyed by pull request and head SHA; a new head SHA marks the draft stale and asks before reuse. Submit calls `submit_review` once with every comment, then clears the draft.
6. **Ship.** `merge` checks the policy (`require_green`, mergeable) and shows a confirmation with the exact title and method, then merges. On success: optional `delete_branch`, local post-merge if the repository is checked out and clean, then a refresh.
7. **Release.** Pick a repository, pick the bump, preview the tag and the notes from `merged_prs_between(last_tag, default_branch_head)`, then `create_release`. Notes follow the configured sections. The default sections are Added, Fixed and Changed, filled from `feat`, `fix` and everything else, each line `<title> by @<author> in #<number>`, followed by New Contributors and a Full Changelog link.

## Configuration

Path: the platform config directory, `%APPDATA%\bridge\config.toml` on Windows, `~/.config/bridge/config.toml` elsewhere, or `--config`. Any key can be overridden by `BRIDGE_<KEY>` with `__` between nested keys.

```toml
roots = ["D:/workspace"]
depth = 2
refresh_seconds = 90

[auth]
source = "keyring"          # keyring | env | device_flow

[[repos]]                   # repositories to watch that are not checked out
name = "owner/name"

[policy.default]
merge.method = "squash"
merge.title = "{title} (#{number})"
merge.body = ""
merge.delete_branch = true
merge.require_green = true
release.tag = "v{version}"
release.bump = "prompt"
sidebar.labels = []
sidebar.milestone = "current"
sidebar.assignee = "me"
sidebar.project = ""

[policy."owner/*"]          # glob on owner/name, most specific wins
merge.method = "rebase"
sidebar.labels = ["needs-review"]

[mute]
repos = ["owner/archive-*"]
authors = ["dependabot[bot]"]
kinds = []
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

- `bridge-core` unit tests for discovery (fixture repositories built with gix in a temp dir), ranking (one table-driven test per rule), templates, sidebar defaults, release notes grouping, and config loading with env overrides.
- Forge tests run against `wiremock` with recorded GitHub REST and GraphQL responses checked into `tests/fixtures/`. No network in the test suite.
- TUI tests render into `ratatui::backend::TestBackend` and compare with `insta` snapshots for each view and for the key flows: open inbox, open thread, reply, triage two selected items, open review, add a comment, submit, confirm merge.
- CI: build and test on `windows-latest`, `ubuntu-latest` and `macos-latest`, clippy with warnings denied, rustfmt check, `cargo-deny` for licenses and advisories. Release workflow builds binaries with checksums for Windows x64 and arm64, Linux x64 and arm64, macOS arm64, and publishes winget and Homebrew manifests.

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

- Name. `bridge` collides with common words and at least one crate. Candidates: `conn` (a ship's conning station), `quarterdeck`, `helm` and `tower` are taken by well-known projects.
- Whether v0.1 should include `PushedBranch` items, which need `list_branches` per repository and cost one request each. Default is on for local repositories only.
- Whether `Mention` should also cover review comment replies on your own pull requests, which GitHub does not count as mentions. Default is yes, found by walking review threads on your open pull requests.
