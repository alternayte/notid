-- name: LockUnusedInvite :one
SELECT code_hash FROM invites
WHERE code_hash = @code_hash AND used_at IS NULL
FOR UPDATE;

-- name: UseInvite :exec
UPDATE invites SET used_by = @used_by, used_at = now()
WHERE code_hash = @code_hash AND used_at IS NULL;

-- name: PersonByHandle :one
SELECT * FROM actors
WHERE kind = 'person' AND domain = @domain AND lower(name) = lower(@name) AND deleted_at IS NULL;

-- name: UpdateProfile :one
UPDATE actors SET display_name = @display_name, summary = @summary, bot = @bot
WHERE id = @id AND local AND kind = 'person'
RETURNING *;

-- name: SoleOwnerOf :one
SELECT c.domain, c.name FROM memberships m JOIN communities c ON c.id = m.community_id
WHERE m.actor_id = @actor_id AND m.role = 'owner'
  AND NOT EXISTS (
    SELECT 1 FROM memberships o
    WHERE o.community_id = m.community_id AND o.role = 'owner' AND o.actor_id <> m.actor_id
  )
LIMIT 1;

-- Deletion tombstones the content so the threads it joined stay readable.
-- name: DeleteOwnPosts :exec
UPDATE posts SET deleted_at = now(), title = '', body = '', url = '', url_image = false, media_id = NULL,
    preview_title = '', preview_description = '', preview_image = ''
WHERE author_id = @author_id AND deleted_at IS NULL;

-- name: DeleteOwnComments :exec
UPDATE comments SET deleted_at = now(), body = ''
WHERE author_id = @author_id AND deleted_at IS NULL;

-- name: DeleteMembershipsOf :exec
DELETE FROM memberships WHERE actor_id = @actor_id;

-- name: AnonymizeActor :exec
UPDATE actors SET
    name = 'deleted_' || id, display_name = '', summary = '', bot = false,
    public_key_pem = '', private_key_enc = NULL, deleted_at = now()
WHERE id = @id AND deleted_at IS NULL;

-- name: ActorsToTrust :many
SELECT a.id FROM actors a
WHERE a.local AND a.kind = 'person' AND a.trust = 'new' AND a.deleted_at IS NULL
  AND a.created_at <= now() - @min_age::interval
  AND (SELECT count(*) FROM posts p
       WHERE p.author_id = a.id AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held)
    + (SELECT count(*) FROM comments m
       WHERE m.author_id = a.id AND m.deleted_at IS NULL AND m.removed_at IS NULL) >= @min_activity
  AND NOT EXISTS (
      SELECT 1 FROM reports r JOIN posts p ON p.id = r.post_id
      WHERE r.state = 'upheld' AND p.author_id = a.id)
  AND NOT EXISTS (
      SELECT 1 FROM reports r JOIN comments m ON m.id = r.comment_id
      WHERE r.state = 'upheld' AND m.author_id = a.id)
  AND NOT EXISTS (
      SELECT 1 FROM bans b
      WHERE b.actor_id = a.id AND b.community_id IS NULL AND (b.expires_at IS NULL OR b.expires_at > now()));

-- name: PromoteActor :exec
UPDATE actors SET trust = 'trusted', promoted_at = now() WHERE id = @id AND trust = 'new';

-- name: SetActorTrust :one
UPDATE actors SET trust = @trust,
    promoted_at = CASE WHEN @trust::text = 'trusted' THEN now() ELSE NULL END
WHERE id = @id AND local AND kind = 'person' AND deleted_at IS NULL
RETURNING *;

-- name: InsertInvite :exec
INSERT INTO invites (code_hash, created_by) VALUES (@code_hash, @created_by);

-- name: UnusedInvites :many
SELECT created_at FROM invites
WHERE created_by = @created_by AND used_at IS NULL
ORDER BY created_at DESC;
