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
