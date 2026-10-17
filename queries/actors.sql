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
