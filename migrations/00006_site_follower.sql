-- +goose Up
-- The site follower is a Service actor on the app domain that follows the
-- directory communities; Lemmy and PieFed take a Follow only from a Person or
-- a Service.
ALTER TABLE actors DROP CONSTRAINT actors_kind_check;
ALTER TABLE actors ADD CONSTRAINT actors_kind_check CHECK (kind IN ('person', 'group', 'application', 'service'));

-- +goose Down
DELETE FROM memberships WHERE actor_id IN (SELECT id FROM actors WHERE kind = 'service');
DELETE FROM actors WHERE kind = 'service';
ALTER TABLE actors DROP CONSTRAINT actors_kind_check;
ALTER TABLE actors ADD CONSTRAINT actors_kind_check CHECK (kind IN ('person', 'group', 'application'));
