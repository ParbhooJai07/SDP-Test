-- Add missing columns to repositories
ALTER TABLE repositories ADD COLUMN remote_url TEXT NOT NULL DEFAULT '';
ALTER TABLE repositories ADD COLUMN local_path TEXT NOT NULL DEFAULT '';
