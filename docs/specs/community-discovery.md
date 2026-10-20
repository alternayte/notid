# Community discovery

## What it does
Search finds communities on other servers that nobody here follows yet, from a community index that notid builds itself.
Search also recognises an address, such as `tech@lemmy.world` or a community URL, and opens that community.
Search and Discover tell people that any `name@server` address opens a community.
An admin sees the same index on the admin page, sorted by active users, and adds a community to the directory from it.
NSFW content stays hidden until a signed-in person turns on NSFW in Settings and confirms they are 18 or older.

## Decisions
- notid builds its own community index from the community lists of servers — no third-party index, so search does not depend on a service we do not run.
- The index reads every peer that is not blocked and runs Lemmy or PieFed, by its nodeinfo, and every server the admin adds under "Index communities from" on the Peers page — an admin can index lemmy.world before anyone federates with it.
- A remote community takes its NSFW flag from its group's `sensitive` field, on every fetch and Update — without it, no remote community counts as NSFW.
- The index takes up to 500 communities per server, 10 pages of 50 in the server's most-active order, ranked by monthly active users — large servers do not fill the table with dead communities.
- Lemmy lists at `/api/v3/community/list` sorted TopMonth; PieFed lists at `/api/alpha/community/list` and has no monthly sort, so notid reads its Top order — each server's own path.
- A River job reads each server once a day and once when the admin adds it; a full read replaces that server's rows — a deleted community leaves within a day.
- A server that fails keeps its last rows, and the Peers page shows its last successful read — one outage does not empty search.
- Blocking a peer deletes its index rows at once — a blocked server is not suggested.
- The index stores the AP ID, name, host, title, description, active users per month, software and the NSFW flag — enough for a search result and nothing to moderate.
- Search shows index results to everyone, read from the stored index — a search sends no request to another server.
- A known community opens its page; a community this site does not know needs a signed-in person to open it, which runs the lookup and queues the backfill — only signed-in people make notid fetch from other servers.
- An address or URL typed in Search opens the community through the same lookup — a person who knows the address needs no second box.
- The NSFW setting is off by default and always off for signed-out visitors.
- With the setting off, NSFW posts and posts in NSFW communities stay out of every list: Home, Popular, All, Search, Discover, the community index and profiles.
- With the setting off, a direct link to an NSFW community or post shows "Marked 18+." with "Sign in to view" or "Turn on NSFW in Settings" — a link from another site is the likely way in.
- Turning the setting on needs a ticked "I am 18 or older" in a dialog; notid stores the time of that confirmation on the person actor; turning it off needs nothing.
- This replaces the front page feeds rule that Popular and All have no setting: they follow the person's NSFW setting.

## Out
- Queries to lemmyverse.net or any other third-party index.
- Indexing Mastodon, notid or other software without a community list.
- Indexing posts; the index holds communities only.
- A per-community NSFW choice, or blurred NSFW media in place of hiding.
- Age verification beyond the person's own confirmation.

## How I know it works
- An admin adds lemmy.world under "Index communities from"; after the job runs, Search for "technology" shows `technology@lemmy.world` with its active users, signed in or out.
- Signed out, that result says "Sign in to open"; signed in, opening it shows the community with its newest posts.
- Typing `tech@programming.dev` in Search opens that community.
- The admin page lists indexed communities by active users, and "Add to directory" makes the site follow it.
- Blocking a peer removes its communities from Search at once.
- With NSFW off, an NSFW post is absent from Home, Popular, All, Search and the community page list, and its link shows "Marked 18+.".
- Turning NSFW on asks for the 18+ confirmation; after it, the same post shows in its lists with the 18+ label.
- `just interop` shows the index reading the Lemmy and PieFed peers.
