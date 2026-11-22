# AGENTS.md

Instructions for coding agents working in this repository. Keep this file current: when a command, the layout or a rule changes, update it in the same commit.

## Project

bridge is a terminal control room for people who maintain many repositories. It builds one ranked inbox of pull requests, issues, review requests, mentions and pushed branches across every repository, and lets you read threads, reply and triage from the terminal. Review, merge and release come next. The design is in `docs/design.md`; plans are in `docs/plans/`; every config key is in `docs/config.md`.

## Commands

```shell
go build ./...
go test ./...
golangci-lint run ./...
make check      # lint plus coverage of 80% or more (leaves out forgetest and tuitest)
make snapshot   # goreleaser build of every archive into dist/
```

On Windows without a C compiler, run `go test ./...` locally. CI runs `-race` on all three systems.

TUI golden files live in each view's `testdata/`. After a deliberate layout change, rewrite them with `go test ./internal/tui/... -update` and read the diff.

## Layout

- `cmd/bridge`: the command. `newRootWith(out, wireOptions)` builds the cobra tree so tests can inject a fake forge, paths, env and clock. `wire.go` loads config, store, token, forge and repositories. No command opens the TUI.
- `forge`: domain types, the `Forge` interface and sentinel errors. `forge/forgetest` is an in-memory `Forge` for tests. `forge/github` implements it with raw GraphQL and REST.
- `config`: YAML, `BRIDGE_*` overrides, policies by glob, mute rules, paths.
- `store`: seen and answered marks, named caches, repository metadata, reply drafts. Writes are atomic.
- `auth`: token from env, keyring or device flow.
- `discover`: finds repositories under the roots and maps remotes to GitHub.
- `inbox`: builds and ranks items (`Build`, `Rank`) and refreshes every source concurrently (`Refresher`).
- `thread`, `triage`: loading threads, replies with drafts, triage changes, issue creation with policy defaults.
- `internal/termsafe`: escapes control characters in forge text. Everything printed or rendered from the forge goes through it.
- `internal/tui`: the bubbletea app. Views are `inboxview`, `threadview`, `triageview` and `issueview`; `msgs` holds the messages between them; `style` holds colors; `tuitest` holds key and golden helpers.

## Rules

- Tests with every change, written first. Table-driven where there is a table. No network in tests. No bare `time.Sleep`.
- Library packages never print, never touch `os.Stdout`, and never import bubbletea.
- No `exec` of `git`, `gh`, a browser or any other program.
- The token never appears in config files, logs, errors or fixtures.
- Text from the forge reaches the terminal only through `internal/termsafe`.
- Paths go through `filepath`, and every test passes on Windows.
- Work on a branch named `feat/<topic>`, `fix/<topic>`, `ci/<topic>`, `docs/<topic>` or `chore/<topic>`. Commit with Conventional Commits messages.
- Merge a squash with `--subject "<pr title> (#<n>)" --body ""` so no co-author trailer lands on main.
- Release with a tag `vX.Y.Z` on main; the release workflow builds archives and takes the notes from that version's CHANGELOG section.
- Never mention AI, assistants or code generation in code, comments, docs, commit messages, pull requests or issues. Never add `Co-Authored-By`, `Generated with` or similar trailers.
- No em-dashes. Keep comments short and about why.
- Update CHANGELOG.md under Unreleased for anything users will notice.
