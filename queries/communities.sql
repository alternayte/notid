-- name: InsertCommunity :one
INSERT INTO communities (actor_id, domain, name, title, description)
VALUES (@actor_id, @domain, @name, @title, @description)
RETURNING *;
