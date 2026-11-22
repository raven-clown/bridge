# Security policy

## Supported versions

Fixes go into the latest release. Please check that a problem still happens on the newest version before reporting it.

## Reporting a vulnerability

Do not open a public issue. Report it privately through GitHub: open the [Security tab](https://github.com/raven-clown/bridge/security), choose "Report a vulnerability", and include:

- what an attacker can do, and what they need first
- steps or a small proof of concept
- the bridge version (`bridge version`), your OS and your terminal

Never include a real GitHub token. Redact it from any logs you attach.

You should get a first answer within 7 days. Once a fix is ready we publish an advisory and a release, and credit you unless you ask us not to.

## What counts

bridge holds a GitHub token and renders text written by other people, so these matter most:

- the token leaking into output, logs, errors, files or network calls to any host other than GitHub
- text from GitHub (titles, comments, diffs, branch names) changing the terminal through control sequences
- a review, merge or triage action reaching a different pull request, commit or repository than the one shown
- files written outside bridge's own config and state directories
