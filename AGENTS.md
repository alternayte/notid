# notid

## What this is
notid is a free, AGPL-3.0, Reddit-like community platform: people sign up on one site and take part in local, Lemmy and PieFed communities without choosing a server.
A community that owns its domain can move to another installation and keep its address and its members.

## Run
No code exists yet.

## Test
No code exists yet.

## Stack rules
- Deploy is Docker Compose on a VPS, for the flagship and for self-hosters, not Kubernetes.
- The web app is TanStack Start with React 19. The Go binary is the only public listener and proxies page requests to it.
- The public API is Huma. Queries go through sqlc, migrations through goose, jobs through River.
- Auth is auth-all, which belongs to the maintainer. Report a defect as an issue on its repo. Never fork, vendor or replace it.

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
