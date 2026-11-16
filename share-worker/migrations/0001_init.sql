-- One row per published page. The page's HTML and its files are in R2:
-- the row only says what is there, so the list and the breadcrumbs are cheap.
CREATE TABLE pages (
  id         TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  icon       TEXT,
  parent_id  TEXT,
  position   INTEGER NOT NULL DEFAULT 0,
  files      TEXT NOT NULL DEFAULT '[]',
  bytes      INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX pages_parent ON pages (parent_id);
