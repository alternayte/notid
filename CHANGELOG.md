# Changelog

All notable changes to notid. Each release has a section here; its body becomes the GitHub release notes.

## [Unreleased]

## [0.1.0-beta.8] - 2026-10-20

- The front page has three feeds: Home, Popular and All. Popular ranks the last week's posts by score and age; All shows them newest first. Both draw from communities here, communities in the directory and communities someone here joined, and both leave out NSFW posts. A signed-out visitor, or a person who has not joined a community yet, lands on Popular. The formulas are on How the feeds work.
- An admin who adds a remote community to the directory makes this site follow it, so its posts reach Popular and All before anyone here joins. Removing it ends the follow. The admin page shows whether the community's server accepted the follow.
- A join or a directory add also fetches the newest comments of each older post, up to 50 per post. PieFed publishes each post's thread; for Lemmy, notid asks Lemmy's API which comments a post has. Every comment is read from its own server.

## [0.1.0-beta.7] - 2026-10-20

- Joining a Lemmy or PieFed community fetches its 20 newest posts, so the community and your feed do not start empty. Each post is read from its own server.

## [0.1.0-beta.6] - 2026-10-20

- A join to a Lemmy or PieFed community shows that it waits for the other server to accept, and changes to Joined without a reload when it does. Home lists the community as waiting to be accepted until then, and reads the feed again once it is.

## [0.1.0-beta.5] - 2026-10-20

- No page scrolls sideways on a phone. Page titles scale with the screen; long words, URLs and handles wrap; code blocks and tables in posts scroll inside the column.

## [0.1.0-beta.4] - 2026-10-19

- A fresh page load is styled from the first paint. The server named a stylesheet that the build never published.
- One account menu at every width, with your email address, your profile, Saved, Start a community, Admin, Settings and Sign out. The masthead keeps one row on a phone, a folding phone and a desktop.
- Your own profile shows the email address you sign in with. Nobody else sees it.

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
