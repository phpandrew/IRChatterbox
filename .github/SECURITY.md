# Security policy

## Reporting a vulnerability

Please **don't open a public issue** for a security problem. Report it privately instead:
**[Security → Report a vulnerability](../../security/advisories/new)** on this repo.

Include what you found, how to reproduce it (a sample IRC message, server or plugin helps a lot), and what an attacker could do with it. You'll get a reply within a few days, and a fix is released through the normal update channel. The in-app updater offers it to everyone automatically.

## Supported versions

Only the latest release gets security fixes. The app updates itself, so staying current is usually just a restart.

## What's in scope

- Anything another person on IRC can do to you by sending messages, topics, nicks, links, images or CTCP requests
- The auto-updater and how it verifies downloads
- Anything that lets a web page or link run code in the app

Plugins you install yourself run with network access by design, so a plugin you chose to install isn't a vulnerability in IRChatterbox.

## How the project checks itself

- **Security workflow**, daily and on every push: `govulncheck` for Go modules, `npm audit` for frontend packages, and CodeQL for our own code. Findings are opened as issues with the `security` label automatically and closed when fixed.
- **Dependabot** opens pull requests for dependency updates.
- **Content Security Policy** in the app: even if a bug let IRC text through as HTML, it can't run script.
