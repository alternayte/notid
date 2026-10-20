-- name: ActorByApID :one
SELECT * FROM actors
WHERE ap_id = @ap_id;

-- name: LocalActorByName :one
SELECT * FROM actors
WHERE local AND kind = @kind AND domain = @domain AND lower(name) = lower(@name);

-- name: CommunityByDomainName :one
SELECT
    c.id, c.actor_id, c.domain, c.name, c.title, c.description, c.theme,
    c.appeals_contact, c.state, c.nsfw, c.hold_new_posts, c.created_at,
    a.ap_id, a.inbox, a.shared_inbox, a.public_key_pem
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE c.domain = @domain AND lower(c.name) = lower(@name);

-- name: LocalPersonCount :one
SELECT count(*) FROM actors WHERE local AND kind = 'person';

-- name: LocalCommunityByApID :one
SELECT c.id, c.state, a.id AS actor_id, a.ap_id
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE a.ap_id = @ap_id AND a.local;

-- name: AcceptFollow :exec
INSERT INTO memberships (community_id, actor_id, role, state, follow_ap_id)
VALUES (@community_id, @actor_id, 'member', 'accepted', @follow_ap_id)
ON CONFLICT (community_id, actor_id) DO UPDATE SET
    state = 'accepted',
    follow_ap_id = excluded.follow_ap_id;

-- name: UndoFollow :exec
DELETE FROM memberships
WHERE community_id = @community_id AND actor_id = @actor_id AND role = 'member';

-- name: RecordActivity :one
INSERT INTO received_activities (ap_id) VALUES (@ap_id)
ON CONFLICT (ap_id) DO NOTHING
RETURNING ap_id;

-- name: PeerByDomain :one
SELECT * FROM peers WHERE domain = @domain;

-- name: PeerOK :exec
INSERT INTO peers (domain, last_ok_at) VALUES (@domain, now())
ON CONFLICT (domain) DO UPDATE SET last_ok_at = now(), failing_since = NULL;

-- name: PeerFailing :exec
INSERT INTO peers (domain, failing_since) VALUES (@domain, now())
ON CONFLICT (domain) DO UPDATE SET failing_since = coalesce(peers.failing_since, now());

-- name: MembershipByFollow :one
SELECT m.community_id, m.actor_id, m.state, g.ap_id AS group_ap_id
FROM memberships m
JOIN communities c ON c.id = m.community_id
JOIN actors g ON g.id = c.actor_id
WHERE m.follow_ap_id = @follow_ap_id;

-- name: PendingFollowOf :one
-- The pending Follow of a person to a group, whichever id it now has.
SELECT m.follow_ap_id
FROM memberships m
JOIN communities c ON c.id = m.community_id
JOIN actors g ON g.id = c.actor_id
JOIN actors p ON p.id = m.actor_id
WHERE g.ap_id = @group_ap_id AND p.ap_id = @person_ap_id AND m.state = 'pending'
    AND m.follow_ap_id IS NOT NULL;

-- name: ReplaceFollow :execrows
UPDATE memberships SET follow_ap_id = @new_ap_id
WHERE follow_ap_id = @old_ap_id AND state = 'pending';

-- name: SetMembershipAccepted :exec
UPDATE memberships SET state = 'accepted' WHERE follow_ap_id = @follow_ap_id;

-- name: DeleteMembershipByFollow :exec
DELETE FROM memberships WHERE follow_ap_id = @follow_ap_id AND role = 'member';

-- name: DeleteMembershipsOfActor :exec
DELETE FROM memberships WHERE actor_id = @actor_id;

-- name: CommunityByGroupApID :one
SELECT c.id, c.state, a.id AS actor_id, a.ap_id, a.local, a.inbox
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE a.ap_id = @ap_id;

-- name: CommunityByID :one
SELECT c.id, c.state, a.id AS actor_id, a.ap_id, a.local, a.inbox
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE c.id = @id;

-- name: FollowerInboxes :many
SELECT DISTINCT coalesce(nullif(a.shared_inbox, ''), a.inbox)::text AS inbox
FROM memberships m
JOIN actors a ON a.id = m.actor_id
WHERE m.community_id = @community_id AND m.state = 'accepted' AND NOT a.local
    AND coalesce(nullif(a.shared_inbox, ''), a.inbox) <> '';

-- Communities that hold a person's content or that the person follows. A
-- person who leaves tells each of their inboxes that the person is gone.
-- name: PersonCommunities :many
SELECT
    c.id, a.local, a.ap_id, a.inbox, coalesce(m.follow_ap_id, '')::text AS follow_ap_id
FROM communities c
JOIN actors a ON a.id = c.actor_id
LEFT JOIN memberships m ON m.community_id = c.id AND m.actor_id = @actor_id
WHERE m.actor_id IS NOT NULL
    OR EXISTS (SELECT 1 FROM posts p WHERE p.community_id = c.id AND p.author_id = @actor_id)
    OR EXISTS (SELECT 1 FROM comments cm JOIN posts p ON p.id = cm.post_id
               WHERE p.community_id = c.id AND cm.author_id = @actor_id);

-- name: PostDoc :one
SELECT p.*, au.ap_id AS author_ap_id, g.ap_id AS group_ap_id
FROM posts p
JOIN actors au ON au.id = p.author_id
JOIN communities c ON c.id = p.community_id
JOIN actors g ON g.id = c.actor_id
WHERE p.ap_id = @ap_id AND p.deleted_at IS NULL;

-- name: CommentDoc :one
SELECT cm.*, au.ap_id AS author_ap_id, g.ap_id AS group_ap_id, p.ap_id AS post_ap_id,
    p.community_id,
    coalesce(pc.ap_id, p.ap_id)::text AS in_reply_to,
    coalesce(pca.ap_id, pa.ap_id)::text AS parent_author_ap_id
FROM comments cm
JOIN actors au ON au.id = cm.author_id
JOIN posts p ON p.id = cm.post_id
JOIN actors pa ON pa.id = p.author_id
JOIN communities c ON c.id = p.community_id
JOIN actors g ON g.id = c.actor_id
LEFT JOIN comments pc ON pc.id = cm.parent_id
LEFT JOIN actors pca ON pca.id = pc.author_id
WHERE cm.ap_id = @ap_id AND cm.deleted_at IS NULL;

-- name: MembershipRole :one
SELECT role FROM memberships
WHERE community_id = @community_id AND actor_id = @actor_id AND state = 'accepted';

-- name: GroupDoc :one
SELECT a.ap_id, c.name, c.title, c.description, a.inbox, a.public_key_pem
FROM communities c JOIN actors a ON a.id = c.actor_id
WHERE c.id = @id;

-- name: SetRemoteMembershipRole :exec
INSERT INTO memberships (community_id, actor_id, role, state)
VALUES (@community_id, @actor_id, @role, 'accepted')
ON CONFLICT (community_id, actor_id) DO UPDATE SET role = excluded.role;

-- name: ObjectKnown :one
SELECT (EXISTS (SELECT 1 FROM posts p WHERE p.ap_id = sqlc.arg(ap_id)::text)
    OR EXISTS (SELECT 1 FROM comments m WHERE m.ap_id = sqlc.arg(ap_id)::text))::boolean AS known;

-- The servers the community index reads: every server an admin chose, and every
-- peer that runs Lemmy or PieFed or whose software notid has not read yet.
-- name: IndexServers :many
SELECT domain FROM peers
WHERE NOT blocked AND (index_source OR software IN ('lemmy', 'piefed', ''))
ORDER BY domain;

-- Inside the rewrite's transaction the lock makes a concurrent block wait, so
-- its delete of the server's rows runs after the rewrite.
-- name: PeerForIndex :one
SELECT domain, software, blocked, index_source FROM peers WHERE domain = @domain FOR UPDATE;

-- name: SetPeerSoftware :exec
UPDATE peers SET software = @software, version = @version WHERE domain = @domain;

-- name: DeleteIndexOfServer :exec
DELETE FROM community_index WHERE server = @server;

-- Rows of servers the index no longer reads.
-- name: DeleteIndexOutside :exec
DELETE FROM community_index WHERE NOT (server = ANY(@servers::text[]));

-- name: InsertIndexEntry :exec
INSERT INTO community_index (ap_id, server, domain, name, title, description, active_month, software, nsfw)
VALUES (@ap_id, @server, @domain, @name, @title, @description, @active_month, @software, @nsfw)
ON CONFLICT (ap_id) DO UPDATE SET server = excluded.server, domain = excluded.domain, name = excluded.name,
    title = excluded.title, description = excluded.description, active_month = excluded.active_month,
    software = excluded.software, nsfw = excluded.nsfw, read_at = now();

-- name: PeerIndexed :exec
UPDATE peers SET indexed_at = now() WHERE domain = @domain;
