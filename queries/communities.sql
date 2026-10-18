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

-- name: DomainServed :one
SELECT EXISTS (
    SELECT 1 FROM community_domains WHERE domain = @domain AND verified_at IS NOT NULL
);
