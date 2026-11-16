export interface Env {
  DB: D1Database;
  FILES: R2Bucket;
  ASSETS: Fetcher;
  /** The shared secret the Notes app sends as `Authorization: Bearer …`. */
  NOTES_TOKEN: string;
  /** Where pages are reached from outside (a custom domain), e.g. https://share.example.com. Optional. */
  PUBLIC_URL?: string;
  /** The largest file taken, in megabytes. */
  MAX_FILE_MB?: string;
}

/** The HTTP API the Notes app speaks. Raised only for a change an older app cannot handle. */
export const API_VERSION = 1;
/** The version of this template. */
export const TEMPLATE_VERSION = "0.1.0";
/** The migrations this version ships (migrations/*.sql); `info` says when the database is behind. */
export const BUNDLED_MIGRATIONS = 1;

/** Page ids are chosen by the app: lowercase letters and digits. */
export const PAGE_ID = /^[a-z0-9]{8,32}$/;
/** File names are chosen by the app: a hash and an extension. */
export const FILE_NAME = /^[A-Za-z0-9_-]{1,80}(\.[A-Za-z0-9]{1,10})?$/;
