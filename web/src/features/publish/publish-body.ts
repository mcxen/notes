import { escapeHtml } from "../../core/inline-content";
import type { DatabaseProperty, NotePage } from "../../core/types";
import { propertyName, propertyValue, statusOf, titleName } from "../database/database-model";
import { bodyToHtml, type HtmlHooks } from "../export/html";
import type { FileRef } from "../editor/adapters";

/** What a published page is made from. */
export interface BodyContext {
  pages: Record<string, NotePage>;
  titleOf(pageId: string): string;
  /** Where a page is on the worker — a path from its root — when it is published or is being; null when it is not. */
  addressOf(pageId: string): string | null;
  /** Where a file of a page is on the worker, or null when it is not sent. */
  fileRef: FileRef | undefined;
  hooks?: Pick<HtmlHooks, "math" | "diagram">;
}

export interface PublishedBody {
  html: string;
  /** How many lines that stand for a page that is not published were left as its name. */
  linksAsText: number;
}

/** An emoji, or nothing: the page's icon may be a drawn one, which the web cannot show. */
export function emojiOf(icon: string | undefined): string {
  return icon && /\p{Extended_Pictographic}/u.test(icon) && icon.length <= 16 ? icon : "";
}

/** The pages under a database, its rows, in the order the database keeps them. */
export function rowsOf(pages: Record<string, NotePage>, databaseId: string): NotePage[] {
  return Object.values(pages)
    .filter((page) => page.parent_id === databaseId && !page.archived)
    .toSorted((a, b) => (a.row_order ?? 0) - (b.row_order ?? 0) || a.title.localeCompare(b.title));
}

const TAGGED = new Set(["status", "select"]);

function cell(row: NotePage, property: DatabaseProperty): string {
  const value = propertyValue(row, property);
  if (!value) return "";
  if (!TAGGED.has(property.type)) return escapeHtml(value);
  // A status wears the colour of its option, as it does in Notes.
  const color = property.type === "status" ? statusOf(property, row.properties?.[property.id] ?? "").color : "default";
  return `<span class="tag tag-${escapeHtml(color)}">${escapeHtml(value)}</span>`;
}

/** A database as a table: the rows as it keeps them, each with its name and its
 *  values; a row is a link when it is published. A picture column is left out (its
 *  files are not sent) and a board, a gallery or a chart is the same table. */
export function databaseTable(database: NotePage, ctx: BodyContext, counted: { text: number }): string {
  const config = database.database;
  if (!config) return "";
  const properties = config.properties.filter((property) => property.type !== "image");
  const head = [titleName(config), ...properties.map(propertyName)].map((name) => `<th>${escapeHtml(name)}</th>`).join("");
  const body = rowsOf(ctx.pages, database.id)
    .map((row) => {
      const name = escapeHtml(ctx.titleOf(row.id) || "Untitled");
      const address = ctx.addressOf(row.id);
      if (!address) counted.text += 1;
      const icon = emojiOf(row.icon);
      const title = `${icon ? `${escapeHtml(icon)} ` : ""}${address ? `<a href="${escapeHtml(encodeURI(address))}">${name}</a>` : name}`;
      return `<tr><td>${title}</td>${properties.map((property) => `<td>${cell(row, property)}</td>`).join("")}</tr>`;
    })
    .join("");
  return `<table class="db"><thead><tr>${head}</tr></thead><tbody>${body}</tbody></table>`;
}

/** The part of a published page the worker puts its title, its trail and its style round. */
export function publishedBody(page: NotePage, ctx: BodyContext): PublishedBody {
  const counted = { text: 0 };
  const hooks: HtmlHooks = {
    ...ctx.hooks,
    page: (pageId, expanded) => {
      const target = ctx.pages[pageId];
      if (!target || target.archived) return "";
      // A database shown in place is its table; any other page is a line with its name.
      if (expanded && target.kind === "database") return databaseTable(target, ctx, counted);
      const name = escapeHtml(ctx.titleOf(pageId) || "Untitled");
      const address = ctx.addressOf(pageId);
      if (!address) counted.text += 1;
      const icon = emojiOf(target.icon);
      return `<p class="page-link">${icon ? `${escapeHtml(icon)} ` : ""}${address ? `<a href="${escapeHtml(encodeURI(address))}">${name}</a>` : name}</p>`;
    },
  };
  let html = bodyToHtml(page.block_tree, ctx.titleOf, ctx.fileRef, hooks);
  // A database page is its table, under whatever is written above it.
  if (page.kind === "database") html += databaseTable(page, ctx, counted);
  return { html, linksAsText: counted.text };
}
