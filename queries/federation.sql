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
