-- name: InstallationID :one
SELECT installation_id FROM installation;

-- name: DomainCommunities :many
SELECT c.id, c.state, c.actor_id, d.domain
FROM communities c
JOIN community_domains d ON d.community_id = c.id
WHERE d.verified_at IS NOT NULL
ORDER BY c.id;

-- name: SetCommunityState :exec
UPDATE communities SET state = @state WHERE id = @id;

-- name: ForgetGroupKey :exec
UPDATE actors SET private_key_enc = NULL, local = false, fetched_at = NULL
WHERE id = @id AND kind = 'group';

-- name: CommunityByDomain :one
SELECT c.id, c.state, c.actor_id, c.name, d.domain
FROM communities c
JOIN community_domains d ON d.community_id = c.id
WHERE d.domain = @domain;

-- name: ExportCommunity :one
SELECT c.id, c.name, c.title, c.description, c.theme, c.appeals_contact, c.nsfw, c.state,
    d.domain, a.id AS actor_id, a.ap_id, a.private_key_enc, a.public_key_pem
FROM communities c
JOIN community_domains d ON d.community_id = c.id
JOIN actors a ON a.id = c.actor_id
WHERE d.domain = @domain AND a.local;

-- name: ExportMembers :many
SELECT a.ap_id, a.kind, a.domain, a.name, a.inbox, a.shared_inbox, m.role, m.follow_ap_id
FROM memberships m
JOIN actors a ON a.id = m.actor_id
WHERE m.community_id = @community_id AND m.state = 'accepted'
ORDER BY a.id;

-- name: ExportPosts :many
SELECT p.ap_id, au.ap_id AS author_ap_id, au.domain AS author_domain, au.name AS author_name,
    p.title, p.body, p.url, p.nsfw, p.published, p.updated, p.score, p.up, p.down
FROM posts p
JOIN actors au ON au.id = p.author_id
WHERE p.community_id = @community_id AND p.deleted_at IS NULL AND p.removed_at IS NULL
ORDER BY p.id;

-- name: ExportComments :many
SELECT cm.ap_id, au.ap_id AS author_ap_id, au.domain AS author_domain, au.name AS author_name,
    p.ap_id AS post_ap_id, pc.ap_id AS parent_ap_id,
    cm.body, cm.published, cm.updated, cm.score, cm.up, cm.down
FROM comments cm
JOIN posts p ON p.id = cm.post_id
JOIN actors au ON au.id = cm.author_id
LEFT JOIN comments pc ON pc.id = cm.parent_id
WHERE p.community_id = @community_id AND cm.deleted_at IS NULL AND cm.removed_at IS NULL
ORDER BY cm.id;

-- name: ActorIDByEmail :one
SELECT a.id FROM actors a
JOIN auth_users u ON u.id = a.user_id
WHERE lower(u.email) = lower(@email::text);

-- name: InsertImportedGroup :one
INSERT INTO actors (ap_id, kind, local, domain, name, display_name, summary, inbox, shared_inbox, public_key_pem, private_key_enc)
VALUES (@ap_id, 'group', true, @domain, @name, @display_name, @summary, @ap_id || '/inbox', @ap_id || '/inbox', @public_key_pem, @private_key_enc)
ON CONFLICT (ap_id) DO UPDATE SET local = true, private_key_enc = excluded.private_key_enc,
    public_key_pem = excluded.public_key_pem, fetched_at = NULL
RETURNING id;

-- name: InsertImportedCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description, theme, appeals_contact, nsfw, state)
VALUES (@actor_id, @domain, @name, @title, @description, @theme, @appeals_contact, @nsfw, 'arriving')
ON CONFLICT (actor_id) DO UPDATE SET state = 'arriving', title = excluded.title, description = excluded.description
RETURNING id;

-- name: InsertImportedDomain :exec
INSERT INTO community_domains (domain, txt_token, created_by, verified_at, community_id)
VALUES (@domain, 'import', @created_by, now(), @community_id)
ON CONFLICT (domain) DO UPDATE SET community_id = excluded.community_id, verified_at = now();

-- name: UpsertStubActor :one
INSERT INTO actors (ap_id, kind, local, domain, name, inbox, shared_inbox)
VALUES (@ap_id, @kind, false, @domain, @name, @inbox, @shared_inbox)
ON CONFLICT (ap_id) DO UPDATE SET ap_id = excluded.ap_id
RETURNING id, local;

-- name: ImportMembership :exec
INSERT INTO memberships (community_id, actor_id, role, state, follow_ap_id)
VALUES (@community_id, @actor_id, @role, 'accepted', @follow_ap_id)
ON CONFLICT (community_id, actor_id) DO UPDATE SET role = excluded.role, state = 'accepted';

-- name: ImportPost :one
INSERT INTO posts (ap_id, ref, local, community_id, author_id, title, body, url, nsfw, published, sort_at, updated, score, up, down)
VALUES (@ap_id, @ref, @local, @community_id, @author_id, @title, @body, @url, @nsfw, @published,
    least(@published::timestamptz, now()), @updated, @score, @up, @down)
ON CONFLICT (ap_id) DO UPDATE SET ap_id = excluded.ap_id
RETURNING id;

-- name: ImportComment :exec
INSERT INTO comments (ap_id, ref, local, post_id, parent_id, author_id, body, published, updated, score, up, down)
VALUES (@ap_id, @ref, @local, @post_id, @parent_id, @author_id, @body, @published, @updated, @score, @up, @down)
ON CONFLICT (ap_id) DO NOTHING;

-- name: PostIDByApID :one
SELECT id FROM posts WHERE ap_id = @ap_id;

-- name: CommentIDByApID :one
SELECT id FROM comments WHERE ap_id = @ap_id;

