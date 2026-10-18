-- name: InsertPost :one
INSERT INTO posts (
    ap_id, ref, local, community_id, author_id, title, body, url, nsfw,
    published, sort_at
) VALUES (
    @ap_id, @ref, @local, @community_id, @author_id, @title, @body, @url, @nsfw,
    @published, least(@published::timestamptz, now())
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
    last_comment_at = greatest(coalesce(last_comment_at, @at::timestamptz), @at::timestamptz)
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
SELECT c.id, c.state, a.local
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE a.ap_id = @ap_id;

-- name: CommunityByID :one
SELECT c.id, c.state, a.local
FROM communities c
JOIN actors a ON a.id = c.actor_id
WHERE c.id = @id;
