-- name: InsertLocalPerson :one
INSERT INTO actors (
    ap_id, kind, local, user_id, domain, name, display_name,
    inbox, shared_inbox, public_key_pem, private_key_enc, trust
) VALUES (
    @ap_id, 'person', true, @user_id, @domain, @name, @display_name,
    @inbox, @shared_inbox, @public_key_pem, @private_key_enc, 'new'
)
ON CONFLICT (user_id) DO NOTHING
RETURNING *;

-- name: ActorByUserID :one
SELECT * FROM actors
WHERE user_id = @user_id;

-- name: LocalNameTaken :one
SELECT EXISTS (
    SELECT 1 FROM actors
    WHERE
        kind = 'person'
        AND domain = @domain
        AND lower(name) = lower(@name::text)
);

-- name: UpsertRemoteActor :one
INSERT INTO actors (
    ap_id, kind, local, domain, name, display_name, summary,
    inbox, shared_inbox, public_key_pem, bot, fetched_at
) VALUES (
    @ap_id, @kind, false, @domain, @name, @display_name, @summary,
    @inbox, @shared_inbox, @public_key_pem, @bot, now()
)
ON CONFLICT (ap_id) DO UPDATE SET
    name = excluded.name,
    display_name = excluded.display_name,
    summary = excluded.summary,
    inbox = excluded.inbox,
    shared_inbox = excluded.shared_inbox,
    public_key_pem = excluded.public_key_pem,
    bot = excluded.bot,
    fetched_at = excluded.fetched_at
WHERE NOT actors.local AND actors.kind = excluded.kind
RETURNING *;

-- name: ActorByApID :one
SELECT * FROM actors
WHERE ap_id = @ap_id;

-- name: ActorByID :one
SELECT * FROM actors
WHERE id = @id;

-- name: InsertApplication :one
INSERT INTO actors (
    ap_id, kind, local, domain, name, inbox, shared_inbox, public_key_pem, private_key_enc
) VALUES (
    @ap_id, 'application', true, @domain, @name, @inbox, @shared_inbox, @public_key_pem, @private_key_enc
)
ON CONFLICT (ap_id) DO NOTHING
RETURNING *;

-- name: InsertLocalGroup :one
INSERT INTO actors (
    ap_id, kind, local, domain, name, display_name, summary,
    inbox, shared_inbox, public_key_pem, private_key_enc
) VALUES (
    @ap_id, 'group', true, @domain, @name, @display_name, @summary,
    @inbox, @shared_inbox, @public_key_pem, @private_key_enc
)
RETURNING *;
