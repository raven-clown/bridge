# bridge

A terminal control room for people who maintain many repositories. One screen shows everything that waits for you across all of them: pull requests to review, issues to triage, comments to answer, fork runs to approve. From that screen you read the thread, reply, label, assign, set a milestone and move items on a project, without opening a browser tab per item.

One static binary. Runs on Windows, macOS and Linux with no tmux, no `gh` and no shell plugins. Free and open source under Apache-2.0, with no telemetry.

Status: v0.1 in progress. Review, merge and release from the terminal come next. See [docs/design.md](docs/design.md).

## Install

Download the archive for your system from the [releases page](https://github.com/raven-clown/bridge/releases) and put `bridge` on your `PATH`, or build it:

```shell
go install github.com/raven-clown/bridge/cmd/bridge@latest
```

Building needs Go 1.26 or newer.

## First run

bridge needs a GitHub token with the `repo`, `workflow`, `read:org` and `project` scopes. Set it in the environment:

```shell
export BRIDGE_TOKEN=ghp_...        # or GITHUB_TOKEN
```

On Windows PowerShell, `$env:BRIDGE_TOKEN = "ghp_..."`. A token in the OS keyring (service `bridge`, user `github.com`) works too.

Tell bridge where your repositories live in the config file, `%APPDATA%\bridge\config.yaml` on Windows or `~/.config/bridge/config.yaml` elsewhere:

```yaml
roots: ["D:/workspace"]
```

Then run `bridge`. Every key is in [docs/config.md](docs/config.md).

## The terminal UI

`bridge` with no command opens one ranked list of everything waiting for you, grouped by kind: fork runs to approve, comments that wait for an answer, your failing pull requests, review requests, untriaged issues, and branches pushed without a pull request.

| Key | In the inbox |
|---|---|
| `enter` | open the thread |
| `t` | triage the item, or every selected item |
| `space` | select |
| `/` | filter by repository or title |
| `n` | new issue |
| `o` | show the URL |
| `x` | hide the item until bridge restarts |
| `R` | refresh now |
| `q` | quit |

In a thread, `j` and `k` move between comments, `r` replies to the comment under the cursor (inside a review thread the reply stays in that thread), `c` writes a new comment, and `ctrl+s` sends. A reply is saved as a draft until GitHub accepts it, so nothing you type is lost.

The triage panel sets labels, milestone, assignees, a project column and open or closed state on one item or many at once.

## Commands

The same operations work without the UI, with `--json` for scripts:

```shell
bridge inbox [--kind review-request] [--repo owner/name] [--json]
bridge thread owner/name#12 [--json]
bridge triage owner/name#12 https://github.com/owner/name/issues/13 --label bug --assign me
bridge triage owner/name#12 --project "Roadmap/In Progress" --milestone v0.1
bridge issue new owner/name --title "Crash on start" --body-file notes.md
bridge auth status
```

`bridge triage` prints one line per item and exits with 1 if any failed. `bridge issue new` fills labels, milestone, assignee and project from your policy, as described in [docs/config.md](docs/config.md).

## Build from source

```shell
go build ./...
go test ./...
make check
```

## License

Apache-2.0
