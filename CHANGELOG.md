# Changelog

All notable changes are listed here. The format follows Keep a Changelog, and versions follow Semantic Versioning.

## Unreleased

### Added

- One ranked inbox across every repository under your roots: your pull requests, pull requests and issues opened against your repositories, review requests, comments waiting for your answer, and branches pushed without a pull request.
- Only clones you can push to count as your repositories, so a workspace full of other projects' clones does not flood the inbox.
- Comments from bots never count as waiting for your answer.
- Fork workflow runs waiting for approval and failing checks show on each pull request, and rank first.
- The terminal UI: inbox with sections, filter and multi-select, a thread view with replies and drafts, a triage panel, and a new issue form.
- Triage from the UI or the `bridge triage` command: labels, milestone, assignees, project column, close and reopen, on one item or many.
- `bridge issue new` fills labels, milestone, assignee and project from per-repository policy.
- `bridge inbox`, `bridge thread` and `bridge triage` print tables or `--json`.
- Token from `BRIDGE_TOKEN`, `GITHUB_TOKEN` or the OS keyring; `bridge auth status` shows where it came from without printing it.
- A token without the project scope still loads everything except projects, and says so.
- Text from GitHub is escaped before it reaches the terminal, so control sequences in titles or comments cannot change your terminal.
- Release archives for Windows, Linux and macOS.
