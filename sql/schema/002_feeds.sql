-- +goose Up
CREATE TABLE feeds (
	id uuid PRIMARY KEY,
	created_at timestamp NOT NULL,
	updated_at timestamp NOT NULL,
	name text NOT NULL,
	url text UNIQUE NOT NULL,
	user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE
);

-- +goose Down
DROP TABLE feeds;
