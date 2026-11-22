# Contributing to bridge

Thanks for wanting to help. Bug reports, ideas, docs fixes and code are all welcome.

## Before you start

- Look through the [open issues](https://github.com/raven-clown/bridge/issues) first. Issues labelled `good first issue` are small and well scoped, and `help wanted` marks work nobody has picked up yet.
- For anything bigger than a small fix, open an issue or a discussion before writing code, so we agree on the approach first.
- Say on the issue that you are working on it, so two people do not do the same work.
- Questions go to [Discussions](https://github.com/raven-clown/bridge/discussions), not issues.
- Security problems never go in a public issue. See [SECURITY.md](SECURITY.md).

## Set up

You need Go 1.26 or newer. `golangci-lint` v2 is optional but CI runs it.

```shell
git clone https://github.com/raven-clown/bridge
cd bridge
go build ./...
go test ./...
golangci-lint run ./...
make check      # lint plus the 80% coverage gate
```

Tests need no network and no GitHub token. To try your build against GitHub, follow "First run" in the [README](README.md).

The design is in [docs/design.md](docs/design.md), and [AGENTS.md](AGENTS.md) lists the package layout and the rules below in more detail.

## Rules for code

- Write the test first. Table-driven where there is a table. No network and no bare `time.Sleep` in tests.
- Every test must pass on Windows, macOS and Linux. Build paths with `filepath`.
- Library packages never print and never import bubbletea. Only `cmd/bridge` and `internal/tui` talk to the terminal.
- Never `exec` git, gh, a browser or any other program.
- Text from GitHub reaches the terminal only through `internal/termsafe`.
- The token never appears in config files, logs, errors, fixtures or issues.
- Keep comments short and about why, not what.
- TUI layouts are checked by golden files in each view's `testdata/`. After a deliberate layout change, run `go test ./internal/tui/... -update` and read the diff before you commit it.

## Pull requests

1. Fork the repository and create a branch named `feat/<topic>`, `fix/<topic>`, `docs/<topic>`, `ci/<topic>` or `chore/<topic>`.
2. Use [Conventional Commits](https://www.conventionalcommits.org/) messages, for example `fix(tui): keep the cursor on screen after a fold`.
3. Add a line to `CHANGELOG.md` under Unreleased for anything users will notice.
4. Keep one topic per pull request. Small pull requests get reviewed faster.
5. Fill in the pull request template and link the issue with `Closes #123`.

CI runs the tests on all three systems, lint, coverage and govulncheck. Workflows from first-time contributors wait for a maintainer to approve them, so a pending check right after you open the pull request is normal.

Pull requests are squash-merged, so the title becomes the commit on main. Please write it as a Conventional Commit too.

## Code of conduct

Everyone taking part agrees to the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

By contributing you agree that your work is licensed under the [Apache License 2.0](LICENSE), the same as the rest of the project.
