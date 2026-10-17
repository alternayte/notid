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
