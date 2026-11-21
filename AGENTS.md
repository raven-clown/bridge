# AGENTS.md

Instructions for coding agents working in this repository. Keep this file current: when a command, the layout or a rule changes, update it in the same commit.

## Project

bridge is a terminal control room for people who maintain many repositories. It builds one ranked inbox of pull requests, issues, review requests, mentions and pushed branches across every repository, and lets you read threads, reply, triage, review, merge and release from the terminal. The design is in `docs/design.md`; the current plan is in `docs/plans/`.

## Commands

```shell
go build ./...
go test ./...
golangci-lint run ./...
make check   # lint plus tests with -race and coverage
```

On Windows without a C compiler, run `go test ./...` locally. CI runs `-race` on all three systems.

## Layout

Only packages that exist are listed.

- `cmd/bridge`: the command. `newRoot(out)` builds the cobra tree so tests can run it.
- `docs/`: design and implementation plans.

## Rules

- Tests with every change, written first. Table-driven where there is a table. No network in tests. No bare `time.Sleep`.
- Library packages never print, never touch `os.Stdout`, and never import bubbletea.
- No `exec` of `git`, `gh` or any other program.
- The token never appears in config files, logs, errors or fixtures.
- Paths go through `filepath`, and every test passes on Windows.
- Work on a branch named `feat/<topic>`, `fix/<topic>`, `ci/<topic>`, `docs/<topic>` or `chore/<topic>`. Commit with Conventional Commits messages.
- Merge a squash with `--subject "<pr title> (#<n>)" --body ""` so no co-author trailer lands on main.
- Never mention AI, assistants or code generation in code, comments, docs, commit messages, pull requests or issues. Never add `Co-Authored-By`, `Generated with` or similar trailers.
- No em-dashes. Keep comments short and about why.
- Update CHANGELOG.md under Unreleased for anything users will notice.
