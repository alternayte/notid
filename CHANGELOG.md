# Changelog

All notable changes to notid. Each release has a section here; its body becomes the GitHub release notes.

## [Unreleased]

## [0.1.0-beta.2] - 2026-10-19

- Runs behind a proxy that terminates TLS: `TRUSTED_PROXIES` lists the proxy's networks, and client addresses for rate limits come from `X-Forwarded-For` only through them. HSTS is sent when the app origin is https.
- Sign-in and sign-up pages follow the design and fit a phone. On a phone, account links sit in a Menu panel.

## [0.1.0-beta.1] - 2026-10-19

First beta. For invited testers; not yet for public self-hosting.

- Sign-in with a magic link. Sign-up modes: open, invite or closed.
- Communities with rules, themes, posts (text, link, image), threaded comments, votes, saves, search and profiles.
- Federation with Lemmy, PieFed and other notid installations: follow, post, comment, vote, edit, delete, moderate and report in both directions, with delivery state shown on each post and comment.
- Moderation: moderator roles, removals, locks, bans, reports, held first posts, and a public moderation log.
- Site admin: peers, blocklist, directory, site bans, trust levels and invites.
- Limits for new accounts: uncounted votes, write limits, upload quotas and federation limits.
- Hardening: SSRF-guarded outbound requests, sanitised Markdown, remote media through a proxy that stores nothing, per-request CSP and security headers.
- Docker images for amd64 and arm64, and a guide to back up and restore an installation.
