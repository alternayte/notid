# notid

## What this is
notid is a free, AGPL-3.0, Reddit-like community platform: people sign up on one site and take part in local, Lemmy and PieFed communities without choosing a server.
A community that owns its domain can move to another installation and keep its address and its members.

## Run
- Self-host or production: copy `.env.example` to `.env`, then `docker compose up -d`.
- Local: `notid migrate`, then `notid all` (listeners and job workers) with env from `.env.example`; set TLS=off and APP_ORIGIN for plain HTTP. `notid serve` runs the listeners without workers.
- Web dev server: `just web-dev`. Point WEB_UPSTREAM at it.
- `notid openapi` prints the API document without a database.

## Test
- `just check` is the gate. It starts a test Postgres in Docker, runs `checks/`, Go vet and tests for changed packages, sqlc, the API client regeneration, and the web typecheck, lint and tests.
- `just test` runs every Go test with `-race`, and every web test.
- DB tests take a rolled-back transaction from `internal/platform/pgtest`. Tests that go through auth-all commit, so they use unique data.

## Stack rules
- Deploy is Docker Compose on a VPS, for the flagship and for self-hosters, not Kubernetes.
- The web app is TanStack Start with React 19. The Go binary is the only public listener and proxies page requests to it.
- The public API is Huma. Queries go through sqlc, migrations through goose, jobs through River.
- SQL lives in `queries/`, one file per feature; sqlc writes each into a db package inside its feature under `internal/`.
- The web app calls the API only through the generated client in `web/src/api`. Never edit its generated files.
- Auth is auth-all, which belongs to the maintainer. Report a defect as an issue on its repo. Never fork, vendor or replace it.
- Mark a temporary workaround for an auth-all issue with one comment that names the issue.

## Domain words
- actor: an identity notid knows, local or remote, either a person or a group. Avoid: account, profile.
- user: a local auth-all account, with exactly one person actor. Avoid: account, member.
- community: a group actor with rules and moderators. Avoid: sub, subreddit, forum.
- app domain: the domain where people sign in and act. Avoid: main domain, primary domain.
- community domain: a verified domain that carries one community's identity. Avoid: custom domain, vanity domain.
- move: the transfer of a community to another installation with the same AP ID. Avoid: migration, transfer.
- installation: one running deployment of notid with its own database. Avoid: instance, node, server.
- peer: a remote server notid federates with, known by its domain. Avoid: instance, remote server.
- AP ID: the ActivityPub URL that identifies an actor, object or activity. Avoid: URI, remote ID, external ID.
- ref: the 12-character permalink key derived from an AP ID. Avoid: slug, short ID, public ID.
- directory: the admin-reviewed list of remote communities that feeds Discover. Avoid: verified list, allowlist.
- departed: the state of a community on the installation it moved away from. Avoid: archived, moved, deleted.
- moderation log: the append-only public record of moderator and admin actions. Avoid: audit log, mod log, journal.
