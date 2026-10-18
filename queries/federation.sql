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

-- name: SetMembershipAccepted :exec
UPDATE memberships SET state = 'accepted' WHERE follow_ap_id = @follow_ap_id;

-- name: DeleteMembershipByFollow :exec
DELETE FROM memberships WHERE follow_ap_id = @follow_ap_id AND role = 'member';

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
