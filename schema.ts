import { schema, t } from "./src/platform/schema";

/** What a paragraph is: a line of words, a heading, a list item, a code
 *  block, a file. A picture, or a link to a page, is never a paragraph of
 *  its own: it stands among a line's words, and a line that holds nothing
 *  else is it on a line of its own. A page lives in its parent, as a file in
 *  a folder (`parent_id`); its words only link to pages. */
export const BLOCK_KINDS = ["text", "heading1", "heading2", "heading3", "todo", "bullet", "numbered", "quote", "callout", "divider", "code", "file", "table", "embed"] as const;

/** Notion's nine colours, in the order Notion lists them. A block or a run of
 *  text takes one as its text colour, or — with `_background` — as a
 *  highlight behind it. */
export const COLOR_NAMES = ["gray", "brown", "orange", "yellow", "green", "blue", "purple", "pink", "red"] as const;
export const CONTENT_COLORS = [...COLOR_NAMES, ...COLOR_NAMES.map((name) => `${name}_background` as const)];

/** The gradients a page's cover may be (features/editor/page-decoration.ts). */
export const COVER_IDS = ["slate", "dawn", "moss", "tide", "orchid", "clay", "ink", "paper"] as const;

/** What a database's property holds, and the layouts of its views. */
export const PROPERTY_TYPES = ["text", "select", "status", "date", "person", "image"] as const;
export const VIEW_KINDS = ["table", "board", "list", "gallery", "calendar"] as const;

/** The member's colour schemes: the system's, or one held. */
export const THEMES = ["system", "light", "dark"] as const;

/** The page list's four views and the file tree's six orders. */
export const SIDEBAR_VIEWS = ["files", "favorites", "shared", "graph"] as const;
export const SIDEBAR_SORTS = ["name", "name-desc", "modified", "modified-asc", "created", "created-asc"] as const;

/** How much history a page keeps: the host retires what is past them. */
export const HISTORY_LIMIT = 20;
export const HISTORY_AGE_MS = 30 * 24 * 60 * 60 * 1000;
export const HISTORY_BUDGET_BYTES = 10 * 1024 * 1024;

/** The host retires a complete checkpoint (all parts) as one entry. */
const HISTORY_RETENTION = {
  groupBy: "page", entryBy: "version", timestamp: "created_at",
  maxAgeSeconds: HISTORY_AGE_MS / 1000, maxEntries: HISTORY_LIMIT, maxBytes: HISTORY_BUDGET_BYTES,
};

/* ── What the commands take ───────────────────────────────────────────────
   `t` fields, each saying what it is to the agent choosing it, and left out
   unless it is required. Field names are the agents': snake_case, as
   Notes' ids. */

/* ── What each record holds ────────────────────────────────────────────
   Its fields, at every depth, as the code writes them. What a record
   schema cannot say is a JSON object here (`t.json`), read through Notes'
   own checks: a card, and the maps a database and a table keep. */

const object = () => t.json({ type: "object" });

/** Bytes a picture or a file paragraph shows, stored beside the pages: in
 *  the member's own storage for a private page, in the space for a shared
 *  one (`space`), each member's apart (`owner`). A picture's own size draws
 *  its box before its bytes arrive; `preview` is a smaller copy beside a
 *  large original. */
const storedFile = t.object({
  path: t.string(), owner: t.string(), space: t.string().optional(), name: t.string(), size: t.number(), type: t.string(),
  width: t.number().optional(), height: t.number().optional(), preview: t.string().optional(),
});

/** A paragraph's own fields, but its words. */
const paragraphFields = {
  id: t.string(),
  kind: t.choice(...BLOCK_KINDS),
  /** The paragraph it is under; none at the top of its page. */
  parent_id: t.string().optional(),
  order: t.number(),
  checked: t.bool().optional(),
  language: t.string().optional(),
  color: t.choice(...CONTENT_COLORS).optional(),
  /** A callout's emoji, in place of the default 💡. */
  icon: t.string().optional(),
  /** A file paragraph's bytes; its caption is its words. */
  file: storedFile.optional(),
  name: t.string().optional(),
  /** How wide a picture is drawn, in CSS pixels; the column's when unset. */
  width: t.number().optional(),
  /** Where its words, pictures among them, stand across the line; unset,
   *  at its start. */
  align: t.choice("center", "right").optional(),
  table: t.object({
    columns: t.list(t.object({ id: t.string(), width: t.number().optional() })),
    rows: object().describe("Its rows by id: each { order, cells }, its cells' words by column id."),
    header_row: t.bool().optional(),
    header_column: t.bool().optional(),
  }).optional(),
  embed: object().optional().describe("A card: the YouTube video or the post on X its address names."),
};

/** A database's properties, as the pages it holds keep their values. */
const property = t.object({
  id: t.string(),
  name: t.string(),
  type: t.choice(...PROPERTY_TYPES),
  options: t.list(t.string()).optional(),
  /** The mark drawn beside the column's name, a key of `PROPERTY_ICONS`;
   *  unset, or unknown to a build, the type's. */
  icon: t.string().optional(),
  /** A status's options, each in its group — not begun, under way,
   *  finished — its pill Notion's grey or one of its nine. Unset, Notion's
   *  three; kept through a change of type, so a Status turned into Text and
   *  back still has its own. */
  status_options: t.list(t.object({
    id: t.string(), name: t.string(), color: t.choice("default", ...COLOR_NAMES), group: t.choice("todo", "progress", "complete"),
  })).optional(),
  /** The option a row reads as until one is chosen; unset, the first To-do. */
  default_status: t.string().optional(),
});

const sort = t.object({ property_id: t.string(), direction: t.choice("asc", "desc") });

/** A saved way of looking at a database. Views belong to the database, so
 *  they are shared with everyone who can see it, as in Notion; which view a
 *  person has open is theirs alone (`database_views`). A column is a
 *  property's id, or the row-name column's (`TITLE_COLUMN`). */
const view = t.object({
  id: t.string(),
  name: t.string(),
  kind: t.choice(...VIEW_KINDS),
  filter: t.object({ property_id: t.string(), value: t.string() }).optional(),
  /** The rows' order, as Notion's sorts: by the first key, rows alike on it
   *  by the next, and so on. */
  sorts: t.list(sort).optional(),
  /** Board: the status or select property whose options are the columns. */
  group_by: t.string().optional(),
  /** Calendar: the date property that places each row on a day. */
  date_by: t.string().optional(),
  /** Table: drawn as a plain Markdown table. */
  minimal: t.bool().optional(),
  /** Gallery: what a card shows above its name — an Image property's first
   *  picture (its id), the page's cover ("cover"), its first lines
   *  ("content") or nothing ("none"); unset, the first Image property, else
   *  the first lines. And the properties a card shows; unset, every one. */
  card_preview: t.string().optional(),
  card_properties: t.list(t.string()).optional(),
  /** Table: each column's width in CSS pixels, set by dragging its edge,
   *  and the columns whose words keep to one line (false), by its "Wrap
   *  text"; every other column wraps. */
  widths: object().optional().describe("Each column's width, by property id."),
  wrap: object().optional().describe("The columns whose words keep to one line (false), by property id."),
});

/** How the graph draws the workspace — Obsidian's graph settings: what it
 *  shows (filters: the pages a query finds, database rows or not, pages
 *  joined to nothing or not, how far around a page), what it colours
 *  (groups), how it draws (arrows, when names show, how big the dots and
 *  how thick the lines are) and how it lays out (forces), and which of the
 *  panel's sections are open. */
const graph = t.object({
  search: t.string(), rows: t.bool(), orphans: t.bool(), depth: t.number(),
  groups: t.list(t.object({ query: t.string(), color: t.choice(...COLOR_NAMES) })),
  arrows: t.bool(), text_fade: t.number(), node_size: t.number(), link_thickness: t.number(),
  center_force: t.number(), repel_force: t.number(), link_force: t.number(), link_distance: t.number(),
  open: t.object({ filters: t.bool(), groups: t.bool(), display: t.bool(), forces: t.bool() }),
});

export default schema({
  collections: {
    // A member's own pages, and the page trees shared into a space: the
    // same collections in either scope. Agents read pages and paragraphs as
    // records: a page's record (its id the page's) holds its name, icon,
    // parent and database; a paragraph's, its page, kind and words.
    pages: {
      scopes: ["personal", "space"],
      fields: {
        id: t.string(),
        title_content: t.richText().describe("Its name: the only place it is kept."),
        icon: t.string(),
        kind: t.choice("document", "database"),
        /** The page it is in; none at the top level. */
        parent_id: t.string().optional(),
        /** A database row's values, words, and its pictures apart from them,
         *  so a column can change type and back with neither kind of value
         *  standing in the other's place. */
        properties: object().optional().describe("A database row's values, by property id."),
        property_files: object().optional().describe("A database row's pictures, by the id of the Image property holding them."),
        database: t.object({
          properties: t.list(property),
          views: t.list(view),
          /** The column holding each row's name — Notion's title property —
           *  and its mark; unset, "Name" and Notion's "Aa". */
          title_name: t.string().optional(),
          title_icon: t.string().optional(),
        }).optional(),
        created_at: t.string(),
        updated_at: t.string(),
        created_by: t.string(),
        updated_by: t.string(),
        archived: t.bool().optional(),
        /** When it went to the trash. */
        archived_at: t.string().optional(),
        /** Where it stands among its database's rows, as a moment in
         *  milliseconds; unset, the moment it was made. */
        row_order: t.number().optional(),
        /** The space it was shared into, on the page shared there. */
        share_space_id: t.string().optional(),
        /** A gradient, or a picture from the member's computer, which wins
         *  over it: whatever sets one clears the other. */
        cover: t.choice(...COVER_IDS).optional(),
        cover_image: storedFile.optional(),
        /** Notion's "Full width" and font, which travel with the page. */
        full_width: t.bool().optional(),
        font: t.choice("default", "serif", "mono").optional(),
      },
      agents: "read",
    },
    blocks: {
      scopes: ["personal", "space"],
      fields: { page: t.string().describe("The page it is a paragraph of."), ...paragraphFields, content: t.richText() },
      agents: "read",
    },
    // A part of a page's checkpoint (src/platform/history-record.ts).
    versions: {
      scopes: ["personal", "space"],
      fields: {
        page: t.string(), version: t.string(), generation: t.string(), created_at: t.string(),
        part: t.int(), parts: t.int(), payload: t.string(),
      },
      retention: HISTORY_RETENTION,
    },
    // The member's own settings, which nobody else sees: `preferences` and
    // `roots` (the top level's order).
    workspace_settings: {
      fields: {
        theme: t.choice(...THEMES).optional(),
        sidebar_width: t.number().optional(),
        sidebar_collapsed: t.bool().optional(),
        database_views: object().optional().describe("The view each database was last open on, by database id."),
        /** Starred pages, in the order they were starred: this member's alone. */
        favorites: t.list(t.string()).optional(),
        sidebar_view: t.choice(...SIDEBAR_VIEWS).optional(),
        sidebar_sort: t.choice(...SIDEBAR_SORTS).optional(),
        graph: graph.optional(),
        roots: t.list(t.string()).optional(),
      },
      agents: "read",
    },
  },
});
