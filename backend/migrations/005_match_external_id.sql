-- Replace the auto-increment PK with the ID from football-data.org
-- so upserts in the seeder can use ON CONFLICT (id).
-- The existing SERIAL still works for hand-jammed rows; we just allow
-- the seeder to supply its own integer IDs.
ALTER TABLE matches ALTER COLUMN id DROP DEFAULT;
DROP SEQUENCE IF EXISTS matches_id_seq;
