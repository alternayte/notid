-- +goose Up
ALTER TABLE posts
    ADD COLUMN url_image boolean NOT NULL DEFAULT false,
    ADD COLUMN preview_title text NOT NULL DEFAULT '',
    ADD COLUMN preview_description text NOT NULL DEFAULT '',
    ADD COLUMN preview_image text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE posts
    DROP COLUMN url_image,
    DROP COLUMN preview_title,
    DROP COLUMN preview_description,
    DROP COLUMN preview_image;
