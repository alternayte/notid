# Front page feeds

## What it does
The front page has three feeds: Home, Popular and All.
Home shows posts from the communities the reader joined.
Popular ranks posts from every community the installation receives by score and age; All shows the same posts newest first.
A visitor who is signed out, or a person with no accepted join, lands on Popular; everyone else lands on Home.
When an admin adds a remote community to the directory, the installation follows it, so its posts reach Popular and All before any local person joins it.
A join or a directory add fetches the community's newest posts and, for each, its newest comments.

## Decisions
- The site follower, a Service actor at `/site` on the app domain, follows each directory community — posts arrive only for followed communities, and a site follow needs no local person.
- The site follower is not the Application actor — Lemmy 0.19 refuses a Follow from an Application, PieFed drops it, and Lemmy reads the Application actor as the site itself.
- The site follow is stored as the site follower's row in memberships and does not count as a member — Accept, Reject and the resend check then work unchanged, and member counts show people.
- Removing a community from the directory undoes the site follow — the installation stops receiving a community no one reviewed or joined.
- Popular and All draw from local communities, directory communities and communities with a local member, without blocked peers — the same set search already uses.
- Popular uses Lemmy's hot rank, `log10(max(1, 3 + score)) / (hours + 2)^1.8`, computed at query time over the last 7 days — no stored column or job at this size.
- The Popular cursor carries the time of the first page — the order does not shift while the reader pages.
- Popular and All leave out NSFW posts and posts in NSFW communities, with no setting — they are the signed-out front page and notid has no age gate.
- The feed is a URL parameter, `/?feed=home|popular|all` — a link opens the same feed.
- Backfill fetches up to 20 posts and, for each post, up to 50 of its newest comments from the post's `context` collection, or its `replies` collection, each read from its origin — older posts arrive with their discussion.
- Lemmy 0.19 publishes neither collection, so for a post at `/post/{id}` notid lists its newest comments through Lemmy's `/api/v3/comment/list` and reads each from its origin — the API only names comments, so it cannot inject content; deleted and removed comments are skipped.
- A directory add queues the same backfill job as a join — one way to fetch history.

## Out
- A bulk import of a peer's communities.
- Any use of the Lemmy or PieFed client APIs beyond listing a Lemmy post's comments.
- Remote scores for backfilled posts and comments; they start at 0.
- An NSFW preference or age gate.
- Per-person filters on Popular, such as hiding a community.

## How I know it works
- Signed out, `/` shows Popular with posts from a directory community that no local person joined.
- A person with no joins lands on Popular; after one join is accepted, `/` lands on Home.
- `/?feed=all` lists posts newest first; a post flagged NSFW appears in its community and not in Popular or All.
- Adding `!technology@lemmy.world` to the directory shows its newest posts in All, and the admin page shows the site follow as accepted.
- Removing it sends an Undo Follow, and new posts from it stop arriving.
- `just interop` shows a directory add on A receiving older posts from Lemmy and PieFed, an older comment from each, and a new post in All.
