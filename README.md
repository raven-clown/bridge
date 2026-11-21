# bridge

A terminal control room for people who maintain many repositories. One screen shows everything that waits for you across all of them: pull requests to review, issues to triage, comments to answer, fork runs to approve. From that screen you read the thread, reply, label, assign, add to a project, review the diff, merge and release, without opening a browser tab per item.

One static binary. Runs on Windows, macOS and Linux with no tmux, no `gh` and no shell plugins.

Status: design done, building v0.1.

## Build from source

Needs Go 1.26 or newer.

```shell
go install github.com/raven-clown/bridge/cmd/bridge@latest
bridge version
```

- Design: [docs/design.md](docs/design.md)
- Current plan: [docs/plans/v0.1-inbox-thread-triage.md](docs/plans/v0.1-inbox-thread-triage.md)
