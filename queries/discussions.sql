-- name: InsertPost :one
INSERT INTO posts (
    ap_id, ref, local, community_id, author_id, title, body, url, media_id, nsfw,
    published, sort_at, held
) VALUES (
    @ap_id, @ref, @local, @community_id, @author_id, @title, @body, @url, sqlc.narg(media_id), @nsfw,
    @published, least(@published::timestamptz, now()), @held
)
ON CONFLICT (ap_id) DO NOTHING
RETURNING *;

-- name: PostByApID :one
SELECT * FROM posts WHERE ap_id = @ap_id;

-- name: PostByID :one
SELECT * FROM posts WHERE id = @id;

-- name: InsertComment :one
INSERT INTO comments (
    ap_id, ref, local, post_id, parent_id, author_id, body, published
) VALUES (
    @ap_id, @ref, @local, @post_id, @parent_id, @author_id, @body, @published
)
ON CONFLICT (ap_id) DO NOTHING
RETURNING *;

-- name: BumpCommentCount :exec
UPDATE posts SET comment_count = comment_count + 1,
    last_comment_at = greatest(coalesce(last_comment_at, least(@at::timestamptz, now())), least(@at::timestamptz, now()))
WHERE id = @id;

-- name: CommentByApID :one
SELECT * FROM comments WHERE ap_id = @ap_id;

-- name: CommentByID :one
SELECT * FROM comments WHERE id = @id;

-- name: PostVote :one
SELECT * FROM votes WHERE actor_id = @actor_id AND post_id = @post_id;

-- name: CommentVote :one
SELECT * FROM votes WHERE actor_id = @actor_id AND comment_id = @comment_id;

-- name: VoteByApID :one
SELECT * FROM votes WHERE ap_id = @ap_id;

-- name: InsertVote :exec
INSERT INTO votes (actor_id, post_id, comment_id, value, counted, ap_id)
VALUES (@actor_id, @post_id, @comment_id, @value, @counted, @ap_id);

-- name: UpdateVote :exec
UPDATE votes SET value = @value, ap_id = @ap_id
WHERE actor_id = @actor_id AND post_id IS NOT DISTINCT FROM @post_id AND comment_id IS NOT DISTINCT FROM @comment_id;

-- name: DeleteVote :exec
DELETE FROM votes
WHERE actor_id = @actor_id AND post_id IS NOT DISTINCT FROM @post_id AND comment_id IS NOT DISTINCT FROM @comment_id;

-- name: AdjustPostScore :exec
UPDATE posts SET up = up + @up_delta, down = down + @down_delta, score = score + (@up_delta::int) - (@down_delta::int) WHERE id = @id;

-- name: AdjustCommentScore :exec
UPDATE comments SET up = up + @up_delta, down = down + @down_delta, score = score + (@up_delta::int) - (@down_delta::int) WHERE id = @id;

-- name: CommunityByGroupApID :one
SELECT c.id, c.state, a.local, c.hold_new_posts
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE a.ap_id = @ap_id;

-- name: CommunityByID :one
SELECT c.id, c.state, a.local, c.hold_new_posts
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE c.id = @id;

-- name: CommunityIDByHandle :one
SELECT c.id FROM communities c WHERE c.domain = @domain AND lower(c.name) = lower(@name);

-- Every post list selects the same columns, so its rows convert to PostListRow.
-- The sort formulas are published on the about pages; change both together.

-- New: newest first by feed time.
-- name: CommunityPostsNew :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.community_id = @community_id AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND (p.sort_at, p.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY p.sort_at DESC, p.id DESC
LIMIT @lim;

-- Active: latest reply first; a post without replies counts from its own feed time.
-- name: CommunityPostsActive :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.community_id = @community_id AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND (p.active_at, p.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY p.active_at DESC, p.id DESC
LIMIT @lim;

-- Top: highest score first, among posts whose feed time is after since.
-- name: CommunityPostsTop :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.community_id = @community_id AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND p.sort_at > @since::timestamptz
  AND (p.score, p.id) < (@before_score::int, @before_id::bigint)
ORDER BY p.score DESC, p.id DESC
LIMIT @lim;

-- Home: posts from the communities the reader joined, newest first by feed time.
-- name: HomeFeed :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
JOIN memberships m ON m.community_id = p.community_id AND m.actor_id = @reader::bigint AND m.state = 'accepted'
WHERE p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND (p.sort_at, p.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY p.sort_at DESC, p.id DESC
LIMIT @lim;

-- name: PostByRef :one
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved, p.id, p.community_id, p.author_id
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.ref = @ref AND p.deleted_at IS NULL AND NOT p.held;

-- Comments of a post in the order they were written. Held comments stay out.
-- name: PostComments :many
SELECT
    m.id, m.ap_id, m.ref, m.parent_id, m.body, m.published, m.score,
    (m.deleted_at IS NOT NULL OR m.removed_at IS NOT NULL)::boolean AS gone,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM comments m
JOIN actors a ON a.id = m.author_id
LEFT JOIN votes v ON v.comment_id = m.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.comment_id = m.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE m.post_id = @post_id AND NOT m.held
ORDER BY m.published, m.id
LIMIT @lim;

-- name: SavePost :exec
INSERT INTO saves (actor_id, post_id) VALUES (@actor_id, @post_id) ON CONFLICT DO NOTHING;

-- name: SaveComment :exec
INSERT INTO saves (actor_id, comment_id) VALUES (@actor_id, @comment_id) ON CONFLICT DO NOTHING;

-- name: UnsavePost :exec
DELETE FROM saves WHERE actor_id = @actor_id AND post_id = @post_id;

-- name: UnsaveComment :exec
DELETE FROM saves WHERE actor_id = @actor_id AND comment_id = @comment_id;

-- Saved posts and comments, newest save first. A saved comment comes with its post.
-- name: SavedItems :many
SELECT
    s.created_at AS saved_at, s.comment_id,
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.score, p.comment_count, p.locked, p.removed_at,
    (p.media_id IS NOT NULL)::boolean AS has_image,
    pa.name AS author_name, pa.domain AS author_domain, pa.display_name AS author_display_name, pa.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(pv.value, 0)::smallint AS my_vote, coalesce(pv.counted, false) AS my_vote_counted,
    EXISTS (SELECT 1 FROM saves ps WHERE ps.post_id = p.id AND ps.actor_id = @viewer)::boolean AS saved,
    m.ap_id AS comment_ap_id, m.ref AS comment_ref, m.parent_id AS comment_parent_id, m.body AS comment_body,
    m.published AS comment_published, m.score AS comment_score,
    ma.name AS comment_author_name, ma.domain AS comment_author_domain,
    ma.display_name AS comment_author_display_name, ma.bot AS comment_author_bot,
    coalesce(mv.value, 0)::smallint AS comment_my_vote, coalesce(mv.counted, false) AS comment_my_vote_counted
FROM saves s
LEFT JOIN comments m ON m.id = s.comment_id
JOIN posts p ON p.id = coalesce(s.post_id, m.post_id)
JOIN actors pa ON pa.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN actors ma ON ma.id = m.author_id
LEFT JOIN votes pv ON pv.post_id = p.id AND pv.actor_id = @viewer
LEFT JOIN votes mv ON mv.comment_id = m.id AND mv.actor_id = @viewer
WHERE s.actor_id = @viewer
  AND p.deleted_at IS NULL AND p.removed_at IS NULL
  AND (m.id IS NULL OR (m.deleted_at IS NULL AND m.removed_at IS NULL))
  AND s.created_at < @before::timestamptz
ORDER BY s.created_at DESC
LIMIT @lim;

-- Search lists matching posts newest first. Content from communities no one
-- here follows stays out of search (federation policy), as it does from Discover.
-- name: SearchPosts :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND p.search @@ websearch_to_tsquery('simple', @query)
  AND (EXISTS (SELECT 1 FROM actors ga WHERE ga.id = c.actor_id AND ga.local)
    OR EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id)
    OR EXISTS (SELECT 1 FROM memberships lm JOIN actors la ON la.id = lm.actor_id
               WHERE lm.community_id = c.id AND la.local AND lm.state = 'accepted'))
  AND (p.sort_at, p.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY p.sort_at DESC, p.id DESC
LIMIT @lim;

-- Every comment list selects the same columns, so its rows convert to SearchCommentsRow.
-- name: SearchComments :many
SELECT
    m.id, m.ap_id, m.ref, m.body, m.published, m.score,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    p.ref AS post_ref, p.title AS post_title, c.name AS community_name, c.domain AS community_domain,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM comments m
JOIN actors a ON a.id = m.author_id
JOIN posts p ON p.id = m.post_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.comment_id = m.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.comment_id = m.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE m.deleted_at IS NULL AND m.removed_at IS NULL AND NOT m.held AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND m.search @@ websearch_to_tsquery('simple', @query)
  AND (EXISTS (SELECT 1 FROM actors ga WHERE ga.id = c.actor_id AND ga.local)
    OR EXISTS (SELECT 1 FROM directory d WHERE d.community_id = c.id)
    OR EXISTS (SELECT 1 FROM memberships lm JOIN actors la ON la.id = lm.actor_id
               WHERE lm.community_id = c.id AND la.local AND lm.state = 'accepted'))
  AND (m.published, m.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY m.published DESC, m.id DESC
LIMIT @lim;

-- name: PersonIDByHandle :one
SELECT id FROM actors WHERE kind = 'person' AND domain = @domain AND lower(name) = lower(@name) AND deleted_at IS NULL;

-- name: AuthorPosts :many
SELECT
    p.id, p.ap_id, p.ref, p.title, p.body, p.url, p.nsfw, p.published, p.sort_at, p.active_at, p.score, p.comment_count, p.locked, p.removed_at, (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.post_id = p.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.post_id = p.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE p.author_id = @author_id AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND (p.sort_at, p.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY p.sort_at DESC, p.id DESC
LIMIT @lim;

-- name: AuthorComments :many
SELECT
    m.id, m.ap_id, m.ref, m.body, m.published, m.score,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    p.ref AS post_ref, p.title AS post_title, c.name AS community_name, c.domain AS community_domain,
    coalesce(v.value, 0)::smallint AS my_vote, coalesce(v.counted, false) AS my_vote_counted,
    (s.actor_id IS NOT NULL)::boolean AS saved
FROM comments m
JOIN actors a ON a.id = m.author_id
JOIN posts p ON p.id = m.post_id
JOIN communities c ON c.id = p.community_id
LEFT JOIN votes v ON v.comment_id = m.id AND v.actor_id = sqlc.narg(viewer)::bigint
LEFT JOIN saves s ON s.comment_id = m.id AND s.actor_id = sqlc.narg(viewer)::bigint
WHERE m.author_id = @author_id AND m.deleted_at IS NULL AND m.removed_at IS NULL AND NOT m.held AND p.deleted_at IS NULL AND p.removed_at IS NULL AND NOT p.held
  AND (m.published, m.id) < (@before_at::timestamptz, @before_id::bigint)
ORDER BY m.published DESC, m.id DESC
LIMIT @lim;

-- Held posts wait for a moderator of their community, oldest first.
-- name: HeldPosts :many
SELECT
    p.id, p.ap_id, p.ref, p.local, p.community_id, p.author_id, p.title, p.body, p.url, p.nsfw,
    p.published, p.received_at, p.sort_at, p.updated, p.locked, p.held, p.deleted_at, p.removed_at,
    p.score, p.up, p.down, p.comment_count, p.last_comment_at,
    (p.media_id IS NOT NULL)::boolean AS has_image,
    a.name AS author_name, a.domain AS author_domain, a.display_name AS author_display_name, a.bot AS author_bot,
    c.name AS community_name, c.domain AS community_domain, c.title AS community_title
FROM posts p
JOIN actors a ON a.id = p.author_id
JOIN communities c ON c.id = p.community_id
WHERE p.community_id = @community_id AND p.held AND p.deleted_at IS NULL
ORDER BY p.published, p.id
LIMIT @lim;

-- name: PublishPost :one
UPDATE posts SET held = false
WHERE ap_id = @ap_id AND held AND deleted_at IS NULL
RETURNING *;

-- name: PostAuthorAndCommunity :one
SELECT p.id, p.author_id, p.community_id, p.ap_id, p.ref
FROM posts p WHERE p.ap_id = @ap_id AND p.held AND p.deleted_at IS NULL;

-- AuthorWritesSince counts a person's local posts and comments since @since,
-- and the subset written in communities another installation hosts.
-- name: AuthorWritesSince :one
SELECT
  (SELECT count(*) FROM posts p
     WHERE p.author_id = @author_id AND p.local AND p.received_at > @since)
+ (SELECT count(*) FROM comments m
     WHERE m.author_id = @author_id AND m.local AND m.received_at > @since) AS writes,
  (SELECT count(*) FROM posts p
     JOIN communities c ON c.id = p.community_id
     JOIN actors g ON g.id = c.actor_id
     WHERE p.author_id = @author_id AND p.local AND NOT g.local AND p.received_at > @since)
+ (SELECT count(*) FROM comments m
     JOIN posts p ON p.id = m.post_id
     JOIN communities c ON c.id = p.community_id
     JOIN actors g ON g.id = c.actor_id
     WHERE m.author_id = @author_id AND m.local AND NOT g.local AND m.received_at > @since) AS remote_writes;
