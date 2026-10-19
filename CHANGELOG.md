# Changelog

All notable changes to notid. Each release has a section here; its body becomes the GitHub release notes.

## [Unreleased]

## [0.1.0-beta.3] - 2026-10-19

- GitHub sign-in and sign-up. Set `GITHUB_CLIENT_ID` and `GITHUB_CLIENT_SECRET` from a GitHub OAuth app whose callback URL is `<APP_ORIGIN>/api/auth/oauth/github/callback`. A new account takes the GitHub username as its name, and asks for another name only when that one is taken or does not fit. Invite-only sites ask for the invite code first. A GitHub login with the same verified email address as an existing account signs in to that account.
- A join to a Lemmy or PieFed community that stays pending is sent again after 5, 10 and 20 minutes. Lemmy drops the answer to the first join from a site it has not seen before.
- Only bot accounts can create API keys, now enforced by auth-all for every way a key is created.

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
