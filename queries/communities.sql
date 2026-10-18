-- name: InsertCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description)
VALUES (@actor_id, @domain, @name, @title, @description)
RETURNING *;

-- name: CommunityByActorID :one
SELECT * FROM communities WHERE actor_id = @actor_id;

-- name: InsertRemoteCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description)
VALUES (@actor_id, @domain, @name, @title, @description)
ON CONFLICT (actor_id) DO UPDATE SET title = excluded.title, description = excluded.description
RETURNING *;

-- name: UpsertMembership :one
INSERT INTO memberships (community_id, actor_id, state, follow_ap_id)
VALUES (@community_id, @actor_id, @state, @follow_ap_id)
ON CONFLICT (community_id, actor_id) DO UPDATE SET
    state = CASE WHEN memberships.state = 'accepted' THEN memberships.state ELSE excluded.state END,
    follow_ap_id = coalesce(excluded.follow_ap_id, memberships.follow_ap_id)
RETURNING *;

-- name: InsertVerifiedDomain :exec
INSERT INTO community_domains (domain, txt_token, created_by, verified_at, community_id)
VALUES (@domain, @txt_token, @created_by, now(), @community_id);

-- name: DomainState :one
SELECT c.state
FROM community_domains d
JOIN communities c ON c.id = d.community_id
WHERE d.domain = @domain AND d.verified_at IS NOT NULL;

-- name: CommunityByHandle :one
SELECT
    c.*, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m WHERE m.community_id = c.id AND m.state = 'accepted')::bigint AS members
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE c.domain = @domain AND lower(c.name) = lower(@name);

-- name: Rules :many
SELECT text FROM rules WHERE community_id = @community_id ORDER BY position;

-- name: DeleteRules :exec
DELETE FROM rules WHERE community_id = @community_id;

-- name: InsertRule :exec
INSERT INTO rules (community_id, position, text) VALUES (@community_id, @position, @text);

-- name: Moderators :many
SELECT a.name, a.domain, a.local, a.display_name, m.role
FROM memberships m
JOIN actors a ON a.id = m.actor_id
WHERE m.community_id = @community_id AND m.role IN ('owner', 'moderator') AND m.state = 'accepted'
ORDER BY m.role DESC, m.created_at;

-- name: MembershipOf :one
SELECT * FROM memberships WHERE community_id = @community_id AND actor_id = @actor_id;

-- name: InsertOwner :exec
INSERT INTO memberships (community_id, actor_id, role, state)
VALUES (@community_id, @actor_id, 'owner', 'accepted');

-- name: DeleteMembership :one
DELETE FROM memberships WHERE community_id = @community_id AND actor_id = @actor_id
RETURNING *;

-- name: CountOwners :one
SELECT count(*) FROM memberships
WHERE community_id = @community_id AND role = 'owner' AND state = 'accepted';

-- name: UpdateCommunity :one
UPDATE communities SET
    title = @title, description = @description, theme = @theme,
    appeals_contact = @appeals_contact, nsfw = @nsfw
WHERE id = @id
RETURNING *;

-- Discover lists communities that live here and the reviewed directory, never
-- other remote communities.
-- name: Discover :many
SELECT
    c.id, c.domain, c.name, c.title, c.description, c.theme, c.nsfw, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m WHERE m.community_id = c.id AND m.state = 'accepted')::bigint AS members,
    my.state AS my_state, my.role AS my_role
FROM communities c
JOIN actors a ON a.id = c.actor_id
LEFT JOIN memberships my ON my.community_id = c.id AND my.actor_id = sqlc.narg(viewer_id)::bigint
WHERE c.state = 'active'
  AND (a.local OR EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id))
ORDER BY members DESC, c.id
LIMIT @lim OFFSET @off;

-- name: Joined :many
SELECT
    c.id, c.domain, c.name, c.title, c.description, c.theme, c.nsfw, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m WHERE m.community_id = c.id AND m.state = 'accepted')::bigint AS members,
    my.state AS my_state, my.role AS my_role
FROM memberships my
JOIN communities c ON c.id = my.community_id
JOIN actors a ON a.id = c.actor_id
WHERE my.actor_id = @actor_id
ORDER BY lower(coalesce(nullif(c.title, ''), c.name));

-- name: CommunityByID :one
SELECT * FROM communities WHERE id = @id;

-- name: CommunityIsLocal :one
SELECT a.local FROM communities c JOIN actors a ON a.id = c.actor_id WHERE c.id = @id;
