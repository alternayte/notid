-- name: ModeratorOf :one
SELECT role FROM memberships
WHERE community_id = @community_id AND actor_id = @actor_id AND state = 'accepted';

-- name: CommunityIDByHandle :one
SELECT c.id FROM communities c WHERE c.domain = @domain AND lower(c.name) = lower(@name);

-- name: PostForModeration :one
SELECT p.id, p.ap_id, p.ref, p.community_id, p.author_id, p.local
FROM posts p WHERE p.ap_id = @ap_id;

-- name: CommentForModeration :one
SELECT m.id, m.ap_id, m.ref, p.community_id, m.author_id, m.local, p.ap_id AS post_ap_id
FROM comments m JOIN posts p ON p.id = m.post_id WHERE m.ap_id = @ap_id;

-- name: RemovePost :one
UPDATE posts SET removed_at = now()
WHERE ap_id = @ap_id AND removed_at IS NULL AND deleted_at IS NULL
RETURNING *;

-- name: RestorePost :one
UPDATE posts SET removed_at = NULL
WHERE ap_id = @ap_id AND removed_at IS NOT NULL
RETURNING *;

-- name: SetPostLocked :one
UPDATE posts SET locked = @locked WHERE ap_id = @ap_id RETURNING *;

-- name: RemoveComment :one
UPDATE comments SET removed_at = now()
WHERE ap_id = @ap_id AND removed_at IS NULL AND deleted_at IS NULL
RETURNING *;

-- name: RestoreComment :one
UPDATE comments SET removed_at = NULL
WHERE ap_id = @ap_id AND removed_at IS NOT NULL
RETURNING *;

-- name: InsertBan :one
INSERT INTO bans (community_id, actor_id, expires_at)
VALUES (@community_id, @actor_id, @expires_at)
ON CONFLICT (community_id, actor_id) DO UPDATE SET expires_at = excluded.expires_at, created_at = now()
RETURNING *;

-- name: DeleteBan :exec
DELETE FROM bans WHERE community_id = @community_id AND actor_id = @actor_id;

-- name: ActiveBan :one
SELECT * FROM bans
WHERE community_id IS NOT DISTINCT FROM @community_id AND actor_id = @actor_id
  AND (expires_at IS NULL OR expires_at > now());

-- name: DeleteMembershipForBan :exec
DELETE FROM memberships WHERE community_id = @community_id AND actor_id = @actor_id;

-- name: DeleteAllMembershipsForBan :exec
DELETE FROM memberships WHERE actor_id = @actor_id;

-- name: DeleteSiteBan :exec
DELETE FROM bans WHERE community_id IS NULL AND actor_id = @actor_id;

-- name: InsertReport :one
INSERT INTO reports (reporter_id, post_id, comment_id, community_id, reason)
VALUES (@reporter_id, sqlc.narg(post_id), sqlc.narg(comment_id), @community_id, @reason)
RETURNING *;

-- Reports wait in the queue while their state is open.
-- name: ReportQueue :many
SELECT
    r.id, r.reason, r.created_at, r.state,
    r.post_id, r.comment_id,
    ra.name AS reporter_name, ra.domain AS reporter_domain,
    p.ref AS post_ref, p.ap_id AS post_ap_id, p.title AS post_title,
    m.ref AS comment_ref, m.body AS comment_body,
    c.name AS community_name, c.domain AS community_domain
FROM reports r
JOIN actors ra ON ra.id = r.reporter_id
JOIN communities c ON c.id = r.community_id
LEFT JOIN posts p ON p.id = r.post_id
LEFT JOIN comments m ON m.id = r.comment_id
WHERE r.community_id = @community_id AND r.state = 'open'
ORDER BY r.created_at
LIMIT @lim;

-- name: ReportCommunity :one
SELECT community_id FROM reports WHERE id = @id;

-- ReportForModeration reads a report with whether its target is local content.
-- name: ReportForModeration :one
SELECT r.community_id, coalesce(p.local, m.local, false)::boolean AS local
FROM reports r
LEFT JOIN posts p ON p.id = r.post_id
LEFT JOIN comments m ON m.id = r.comment_id
WHERE r.id = @id;

-- The site queue holds open reports on content a local person wrote, in any community.
-- name: SiteReportQueue :many
SELECT
    r.id, r.reason, r.created_at, r.state,
    r.post_id, r.comment_id,
    ra.name AS reporter_name, ra.domain AS reporter_domain,
    p.ref AS post_ref, p.ap_id AS post_ap_id, p.title AS post_title,
    m.ref AS comment_ref, m.body AS comment_body,
    c.name AS community_name, c.domain AS community_domain
FROM reports r
JOIN actors ra ON ra.id = r.reporter_id
JOIN communities c ON c.id = r.community_id
LEFT JOIN posts p ON p.id = r.post_id
LEFT JOIN comments m ON m.id = r.comment_id
WHERE r.state = 'open'
  AND ((p.local) OR (m.local))
ORDER BY r.created_at
LIMIT @lim;

-- Site bans have no community, and their reason is the newest site_ban log line.
-- name: SiteBans :many
SELECT b.actor_id, a.name, a.domain, b.expires_at, b.created_at,
       coalesce((
         SELECT l.reason FROM moderation_log l
         WHERE l.community_id IS NULL AND l.target_actor_id = b.actor_id AND l.action = 'site_ban'
         ORDER BY l.created_at DESC, l.id DESC LIMIT 1
       ), '')::text AS reason
FROM bans b JOIN actors a ON a.id = b.actor_id
WHERE b.community_id IS NULL AND (b.expires_at IS NULL OR b.expires_at > now())
ORDER BY b.created_at DESC;

-- name: ResolveReport :one
UPDATE reports SET state = @state, resolved_by = @resolved_by, resolved_at = now()
WHERE id = @id AND state = 'open'
RETURNING *;

-- name: InsertModerationLog :one
INSERT INTO moderation_log (community_id, moderator_id, action, target_actor_id, target_ap_id, reason, expires_at)
VALUES (@community_id, @moderator_id, @action, sqlc.narg(target_actor_id), sqlc.narg(target_ap_id), @reason, sqlc.narg(expires_at))
RETURNING *;

-- The log is public; it names the moderator and the target actor.
-- name: ModerationLog :many
SELECT
    l.id, l.action, l.reason, l.expires_at, l.created_at, l.target_ap_id,
    m.name AS moderator_name, m.domain AS moderator_domain, m.display_name AS moderator_display_name,
    t.name AS target_name, t.domain AS target_domain, t.display_name AS target_display_name
FROM moderation_log l
JOIN actors m ON m.id = l.moderator_id
LEFT JOIN actors t ON t.id = l.target_actor_id
WHERE l.community_id = @community_id
ORDER BY l.created_at DESC, l.id DESC
LIMIT @lim;

-- name: ActorIDByHandle :one
SELECT id FROM actors
WHERE domain = @domain AND lower(name) = lower(@name) AND deleted_at IS NULL AND kind = 'person';

-- name: ModerationCounts :many
SELECT action,
    count(*)::int AS total,
    count(*) FILTER (WHERE created_at >= now() - @since::interval)::int AS recent
FROM moderation_log
GROUP BY action
ORDER BY action;

-- name: ReportCounts :one
SELECT
    count(*)::int AS filed,
    count(*) FILTER (WHERE state = 'upheld')::int AS upheld,
    count(*) FILTER (WHERE state = 'dismissed')::int AS dismissed,
    count(*) FILTER (WHERE created_at >= now() - @since::interval)::int AS filed_recent,
    count(*) FILTER (WHERE state = 'upheld' AND resolved_at >= now() - @since::interval)::int AS upheld_recent,
    count(*) FILTER (WHERE state = 'dismissed' AND resolved_at >= now() - @since::interval)::int AS dismissed_recent
FROM reports;
