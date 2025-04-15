-- +goose Up
CREATE TABLE feed_follows (
	id uuid PRIMARY KEY,
	created_at timestamp NOT NULL,
	updated_at timestamp NOT NULL,
	user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
	feed_id uuid NOT NULL REFERENCES feeds ON DELETE CASCADE,
	CONSTRAINT no_dupes UNIQUE(user_id, feed_id)
);

-- +goose Down
DROP TABLE feed_follows;
