-- name: CreatePost :one
INSERT INTO posts
(id, created_at, updated_at, title, url, description, published_at, feed_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetPostsForUser :many
SELECT posts.* FROM
feed_follows
	INNER JOIN users ON users.id = feed_follows.user_id
	INNER JOIN posts ON feed_follows.feed_id = posts.feed_id
WHERE users.id = $1
ORDER BY posts.published_at DESC
LIMIT $2;
