# Configuration

bridge reads one YAML file:

- Windows: `%APPDATA%\bridge\config.yaml`
- macOS and Linux: `$XDG_CONFIG_HOME/bridge/config.yaml`, or `~/.config/bridge/config.yaml`
- or any path given with `--config`, or in `BRIDGE_CONFIG`

A missing or empty file means every default below. An unknown key is an error, so a typo does not pass silently.

Any key can be set from the environment as `BRIDGE_` plus the key in upper case, with `__` between nested keys. Lists take commas. Examples: `BRIDGE_DEPTH=3`, `BRIDGE_AUTH__SOURCE=env`, `BRIDGE_ROOTS=D:/a,D:/b`, `BRIDGE_POLICY__DEFAULT__MERGE__METHOD=rebase`. Only the `default` policy can be set this way, since environment names cannot hold `/` or `*`.

## Keys

| Key | Default | Meaning |
|---|---|---|
| `roots` | none | Folders to search for git repositories |
| `depth` | `2` | How many folder levels below each root to search |
| `refresh_seconds` | `90` | How often the UI refreshes, at least 10 |
| `auth.source` | `keyring` | `keyring` (env, then keyring), `env` (env only) or `device_flow` |
| `repos` | none | Extra repositories to watch, as `owner/name` or `host/owner/name` |
| `mute.repos` | none | Glob patterns on `owner/name` to leave out of the inbox |
| `mute.authors` | none | Logins to leave out, such as `dependabot[bot]` |
| `mute.kinds` | none | Kinds to leave out: `own-pr`, `incoming-pr`, `incoming-issue`, `review-request`, `mention`, `pushed-branch` |
| `confirm.comment` | `false` | Ask before sending a comment |
| `confirm.triage` | `false` | Ask before applying a triage change |
| `confirm.close` | `true` | Ask before closing items |
| `confirm.merge`, `confirm.release`, `confirm.review` | `true` | For the review and ship features that come next |

## Policies

`policy` maps a repository pattern to settings. `default` applies everywhere. Other keys are glob patterns on `owner/name`, such as `raven-clown/*` or `raven-clown/airlock`. Every matching pattern is layered over `default`, least specific first, so a repository-wide rule and a single-repository rule combine.

| Key | Default | Meaning |
|---|---|---|
| `sidebar.labels` | none | Labels for new issues |
| `sidebar.milestone` | none | `current` (the open milestone due soonest), a milestone title, or `none` |
| `sidebar.assignee` | none | `me`, a login, or `none` |
| `sidebar.project` | none | Project title new issues are added to |
| `merge.method` | `squash` | `squash`, `merge` or `rebase` |
| `merge.title` | `{title} (#{number})` | Commit title for a merge |
| `merge.body` | empty | Commit body; empty keeps co-author trailers off a squash |
| `merge.delete_branch` | `true` | Delete the branch after merging |
| `merge.require_green` | `true` | Refuse to merge with failing checks |
| `release.tag` | `v{version}` | Tag template |
| `release.bump` | `prompt` | `minor`, `patch` or `prompt` |

The `merge` and `release` keys are read now and used by the ship features in the next release.

## Example

```yaml
roots: ["D:/workspace"]
repos: [raven-clown/mothership]

policy:
  default:
    sidebar:
      milestone: current
      assignee: me
  "raven-clown/*":
    sidebar:
      labels: [needs-triage]
      project: Roadmap

mute:
  authors: ["dependabot[bot]"]
```

## Token

bridge looks for a token in `BRIDGE_TOKEN`, then `GITHUB_TOKEN`, then the OS keyring (Windows Credential Manager, macOS Keychain or Secret Service) under service `bridge` and user `github.com`. The token is never written to the config file or the logs. It needs the `repo`, `workflow`, `read:org` and `project` scopes; without `project`, everything works except moving items on projects.

## Local state

Caches, seen marks and reply drafts live in `%LOCALAPPDATA%\bridge` on Windows and `~/.local/state/bridge` elsewhere, or `BRIDGE_STATE_DIR`. Deleting that folder loses nothing on GitHub.
