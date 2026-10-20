-- name: InsertCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description)
VALUES (@actor_id, @domain, @name, @title, @description)
RETURNING *;

-- name: CommunityByActorID :one
SELECT * FROM communities WHERE actor_id = @actor_id;

-- A remote community takes its NSFW flag from its group's sensitive flag.
-- name: InsertRemoteCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description, nsfw)
VALUES (@actor_id, @domain, @name, @title, @description, (SELECT a.sensitive FROM actors a WHERE a.id = @actor_id))
ON CONFLICT (actor_id) DO UPDATE SET title = excluded.title, description = excluded.description, nsfw = excluded.nsfw
RETURNING *;

-- name: SyncRemoteCommunityNSFW :exec
UPDATE communities c SET nsfw = a.sensitive
FROM actors a
WHERE a.id = c.actor_id AND a.id = @actor_id AND NOT a.local;

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
    (SELECT count(*) FROM memberships m JOIN actors ma ON ma.id = m.actor_id
     WHERE m.community_id = c.id AND m.state = 'accepted' AND ma.kind = 'person')::bigint AS members
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
    appeals_contact = @appeals_contact, nsfw = @nsfw, hold_new_posts = @hold_new_posts
WHERE id = @id
RETURNING *;

-- Discover lists communities that live here and the reviewed directory, never
-- other remote communities.
-- name: Discover :many
SELECT
    c.id, c.domain, c.name, c.title, c.description, c.theme, c.nsfw, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m JOIN actors ma ON ma.id = m.actor_id
     WHERE m.community_id = c.id AND m.state = 'accepted' AND ma.kind = 'person')::bigint AS members,
    my.state AS my_state, my.role AS my_role
FROM communities c
JOIN actors a ON a.id = c.actor_id
LEFT JOIN memberships my ON my.community_id = c.id AND my.actor_id = sqlc.narg(viewer_id)::bigint
WHERE c.state = 'active'
  AND NOT EXISTS (SELECT 1 FROM peers pe WHERE pe.domain = c.domain AND pe.blocked)
  AND (a.local OR EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id))
  AND (NOT c.nsfw OR EXISTS (SELECT 1 FROM actors va WHERE va.id = sqlc.narg(viewer_id)::bigint AND va.nsfw_confirmed_at IS NOT NULL))
ORDER BY members DESC, c.id
LIMIT @lim OFFSET @off;

-- name: Joined :many
SELECT
    c.id, c.domain, c.name, c.title, c.description, c.theme, c.nsfw, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m JOIN actors ma ON ma.id = m.actor_id
     WHERE m.community_id = c.id AND m.state = 'accepted' AND ma.kind = 'person')::bigint AS members,
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

-- Search finds communities that live here, are in the directory, or have a
-- local member; communities no one here follows stay out (federation policy).
-- name: SearchCommunities :many
SELECT
    c.id, c.domain, c.name, c.title, c.description, c.theme, c.nsfw, a.ap_id, a.local,
    (SELECT count(*) FROM memberships m JOIN actors ma ON ma.id = m.actor_id
     WHERE m.community_id = c.id AND m.state = 'accepted' AND ma.kind = 'person')::bigint AS members,
    my.state AS my_state, my.role AS my_role
FROM communities c
JOIN actors a ON a.id = c.actor_id
LEFT JOIN memberships my ON my.community_id = c.id AND my.actor_id = sqlc.narg(viewer_id)::bigint
WHERE c.state = 'active'
  AND NOT EXISTS (SELECT 1 FROM peers pe WHERE pe.domain = c.domain AND pe.blocked)
  AND c.search @@ websearch_to_tsquery('simple', @query)
  AND (a.local
    OR EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id)
    OR EXISTS (SELECT 1 FROM memberships lm JOIN actors la ON la.id = lm.actor_id
               WHERE lm.community_id = c.id AND la.local AND lm.state = 'accepted'))
  AND (NOT c.nsfw OR EXISTS (SELECT 1 FROM actors va WHERE va.id = sqlc.narg(viewer_id)::bigint AND va.nsfw_confirmed_at IS NOT NULL))
ORDER BY members DESC, c.id
LIMIT @lim;

-- name: UpdateRemoteCommunity :one
UPDATE communities c SET title = @title, description = @description, nsfw = @nsfw
FROM actors a
WHERE a.id = c.actor_id AND a.ap_id = @ap_id AND NOT a.local
RETURNING c.*;

-- The directory is the admin-reviewed list of remote communities Discover shows.
-- name: Directory :many
SELECT c.id, c.domain, c.name, c.title, d.note, d.created_at,
    coalesce((SELECT sf.state FROM memberships sf WHERE sf.community_id = c.id AND sf.actor_id = @follower_id), 'none')::text AS site_follow,
    (SELECT count(*) FROM memberships m JOIN actors ma ON ma.id = m.actor_id
     WHERE m.community_id = c.id AND m.state = 'accepted' AND ma.kind = 'person')::bigint AS members
FROM directory d
JOIN communities c ON c.id = d.community_id
ORDER BY d.created_at DESC, c.id
LIMIT @lim;

-- name: AddToDirectory :exec
INSERT INTO directory (community_id, added_by, note) VALUES (@community_id, @added_by, @note)
ON CONFLICT (community_id) DO UPDATE SET note = excluded.note;

-- name: RemoveFromDirectory :exec
DELETE FROM directory WHERE community_id = @community_id;

-- The community index is what notid read from the community lists of Lemmy and
-- PieFed servers. Search shows the entries that the search above leaves out:
-- communities nobody here follows. known marks the ones this site has fetched.
-- name: SearchIndex :many
SELECT i.ap_id, i.domain, i.name, i.title, i.description, i.active_month, i.software, i.nsfw,
    EXISTS (SELECT 1 FROM actors ka JOIN communities kc ON kc.actor_id = ka.id WHERE ka.ap_id = i.ap_id)::boolean AS known
FROM community_index i
WHERE i.search @@ websearch_to_tsquery('simple', @query)
  AND NOT EXISTS (SELECT 1 FROM peers pe WHERE pe.domain = i.domain AND pe.blocked)
  AND (NOT i.nsfw OR EXISTS (SELECT 1 FROM actors va WHERE va.id = sqlc.narg(viewer_id)::bigint AND va.nsfw_confirmed_at IS NOT NULL))
  AND NOT EXISTS (
    SELECT 1 FROM actors ga JOIN communities c ON c.actor_id = ga.id
    WHERE ga.ap_id = i.ap_id
      AND (EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id)
        OR EXISTS (SELECT 1 FROM memberships lm JOIN actors la ON la.id = lm.actor_id
                   WHERE lm.community_id = c.id AND la.local AND lm.state = 'accepted')))
ORDER BY i.active_month DESC, i.ap_id
LIMIT @lim;

-- The admin's view of the index: most active first, optionally matching words.
-- name: AdminIndex :many
SELECT i.ap_id, i.domain, i.name, i.title, i.description, i.active_month, i.software, i.nsfw,
    EXISTS (SELECT 1 FROM actors ga JOIN communities c ON c.actor_id = ga.id
            JOIN directory d ON d.community_id = c.id WHERE ga.ap_id = i.ap_id)::boolean AS in_directory
FROM community_index i
WHERE (@query::text = '' OR i.search @@ websearch_to_tsquery('simple', @query::text))
  AND NOT EXISTS (SELECT 1 FROM peers pe WHERE pe.domain = i.domain AND pe.blocked)
  AND (NOT i.nsfw OR EXISTS (SELECT 1 FROM actors va WHERE va.id = @viewer_id AND va.nsfw_confirmed_at IS NOT NULL))
ORDER BY i.active_month DESC, i.ap_id
LIMIT @lim OFFSET @off;
