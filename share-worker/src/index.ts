import { API_VERSION, BUNDLED_MIGRATIONS, FILE_NAME, PAGE_ID, TEMPLATE_VERSION, type Env } from "./env";
import { canonicalPath, HOME_HTML, idOfPath, NOT_FOUND_HTML, PAGE_HEADERS, pageUrl, renderPage, type Crumb, type PageRow } from "./page";

const MAX_HTML = 3_000_000;

function json(body: unknown, status = 200): Response {
  return Response.json(body, { status, headers: { "cache-control": "no-store" } });
}

function fail(status: number, message: string): Response {
  return json({ error: message }, status);
}

/** Whether the request carries the shared token. Both sides are hashed first, so
 *  the comparison takes the same time whatever the token's length. */
async function authorized(request: Request, env: Env): Promise<boolean> {
  const match = /^Bearer\s+(.+)$/i.exec(request.headers.get("authorization") ?? "");
  if (!env.NOTES_TOKEN || !match) return false;
  const encoder = new TextEncoder();
  const [given, expected] = await Promise.all([
    crypto.subtle.digest("SHA-256", encoder.encode(match[1].trim())),
    crypto.subtle.digest("SHA-256", encoder.encode(env.NOTES_TOKEN)),
  ]);
  return (crypto.subtle as SubtleCrypto & { timingSafeEqual(a: ArrayBuffer, b: ArrayBuffer): boolean }).timingSafeEqual(given, expected);
}

const filesKey = (id: string, name: string) => `files/${id}/${name}`;
const htmlKey = (id: string) => `pages/${id}.html`;

/** Deletes every file of a page except those in `keep`. */
async function pruneFiles(env: Env, id: string, keep: Set<string>): Promise<void> {
  const prefix = `files/${id}/`;
  let cursor: string | undefined;
  do {
    const listing = await env.FILES.list({ prefix, cursor });
    const stale = listing.objects.map((object) => object.key).filter((key) => !keep.has(key.slice(prefix.length)));
    if (stale.length) await env.FILES.delete(stale);
    cursor = listing.truncated ? listing.cursor : undefined;
  } while (cursor);
}

async function missingFiles(env: Env, id: string, names: string[]): Promise<string[]> {
  const heads = await Promise.all(names.map((name) => env.FILES.head(filesKey(id, name))));
  return names.filter((_, index) => !heads[index]);
}

interface PageBody {
  title: string;
  html: string;
  icon: string | null;
  parent: string | null;
  position: number;
  files: string[];
}

function readBody(input: unknown, id: string): PageBody | string {
  if (typeof input !== "object" || input === null) return "The body is not an object.";
  const body = input as Record<string, unknown>;
  const title = typeof body.title === "string" ? body.title.trim() : "";
  if (!title || title.length > 300) return "title must be 1 to 300 characters.";
  if (typeof body.html !== "string" || body.html.length > MAX_HTML) return `html must be a string of at most ${MAX_HTML} characters.`;
  const icon = body.icon == null || body.icon === "" ? null : typeof body.icon === "string" && body.icon.length <= 16 ? body.icon : undefined;
  if (icon === undefined) return "icon must be a short string.";
  const parent = body.parent == null ? null : typeof body.parent === "string" && PAGE_ID.test(body.parent) && body.parent !== id ? body.parent : undefined;
  if (parent === undefined) return "parent must be another page's id.";
  const position = body.position == null ? 0 : Number.isInteger(body.position) ? (body.position as number) : NaN;
  if (Number.isNaN(position)) return "position must be an integer.";
  const files = body.files == null ? [] : Array.isArray(body.files) && body.files.length <= 500 && body.files.every((name) => typeof name === "string" && FILE_NAME.test(name)) ? [...new Set(body.files as string[])] : undefined;
  if (files === undefined) return "files must be at most 500 file names.";
  return { title, html: body.html, icon, parent, position, files };
}

async function api(request: Request, env: Env, url: URL): Promise<Response> {
  if (!env.NOTES_TOKEN) return fail(500, "This worker has no NOTES_TOKEN secret. Run the deploy script again.");
  if (!(await authorized(request, env))) return fail(401, "The token is missing or wrong.");
  const path = url.pathname.replace(/^\/api\/v1\/?/, "");
  const method = request.method;

  if (path === "info" && method === "GET") {
    let applied = 0;
    let pages = 0;
    try {
      applied = (await env.DB.prepare("SELECT COUNT(*) AS n FROM d1_migrations").first<{ n: number }>())?.n ?? 0;
    } catch {
      // No migration is applied yet: the bookkeeping table is not there.
    }
    try {
      pages = (await env.DB.prepare("SELECT COUNT(*) AS n FROM pages").first<{ n: number }>())?.n ?? 0;
    } catch {}
    return json({
      app: "notes-share",
      apiVersion: API_VERSION,
      version: TEMPLATE_VERSION,
      migrations: { applied, bundled: BUNDLED_MIGRATIONS, pending: Math.max(0, BUNDLED_MIGRATIONS - applied) },
      pages,
    });
  }

  if (path === "pages" && method === "GET") {
    const { results } = await env.DB.prepare("SELECT * FROM pages ORDER BY updated_at DESC LIMIT 1000").all<PageRow>();
    return json({
      pages: results.map((row) => ({ id: row.id, title: row.title, parent: row.parent_id, updatedAt: new Date(row.updated_at * 1000).toISOString(), url: pageUrl(env, request, row.title, row.id) })),
    });
  }

  const file = /^pages\/([^/]+)\/files\/([^/]+)$/.exec(path);
  if (file && method === "PUT") {
    const [, id, name] = file;
    if (!PAGE_ID.test(id) || !FILE_NAME.test(name)) return fail(400, "That is not a file name.");
    const row = await env.DB.prepare("SELECT files FROM pages WHERE id = ?").bind(id).first<{ files: string }>();
    if (!row) return fail(404, "Publish the page before uploading its files.");
    if (!(JSON.parse(row.files) as string[]).includes(name)) return fail(409, "The page does not list that file.");
    const limit = (Number(env.MAX_FILE_MB) || 25) * 1024 * 1024;
    const length = Number(request.headers.get("content-length"));
    if (length > limit) return fail(413, "That file is larger than this worker takes.");
    const type = request.headers.get("content-type") ?? "application/octet-stream";
    let size: number;
    if (length > 0 && request.body) {
      size = (await env.FILES.put(filesKey(id, name), request.body, { httpMetadata: { contentType: type } })).size;
    } else {
      const bytes = await request.arrayBuffer();
      if (bytes.byteLength > limit) return fail(413, "That file is larger than this worker takes.");
      size = (await env.FILES.put(filesKey(id, name), bytes, { httpMetadata: { contentType: type } })).size;
    }
    return json({ name, size }, 201);
  }

  const page = /^pages\/([^/]+)$/.exec(path);
  if (page) {
    const id = page[1];
    if (!PAGE_ID.test(id)) return fail(400, "That is not a page id.");
    if (method === "GET") {
      const row = await env.DB.prepare("SELECT * FROM pages WHERE id = ?").bind(id).first<PageRow>();
      if (!row) return fail(404, "No page is published with that id.");
      return json({ id, title: row.title, parent: row.parent_id, files: JSON.parse(row.files), url: pageUrl(env, request, row.title, id) });
    }
    if (method === "PUT") {
      let input: unknown;
      try {
        input = await request.json();
      } catch {
        return fail(400, "The body is not JSON.");
      }
      const body = readBody(input, id);
      if (typeof body === "string") return fail(400, body);
      const now = Math.floor(Date.now() / 1000);
      const existing = await env.DB.prepare("SELECT created_at FROM pages WHERE id = ?").bind(id).first<{ created_at: number }>();
      await env.FILES.put(htmlKey(id), body.html, { httpMetadata: { contentType: "text/html; charset=utf-8" } });
      await env.DB.prepare(
        `INSERT INTO pages (id, title, icon, parent_id, position, files, bytes, created_at, updated_at)
         VALUES (?1, ?2, ?3, ?4, ?5, ?6, ?7, ?8, ?8)
         ON CONFLICT(id) DO UPDATE SET title = ?2, icon = ?3, parent_id = ?4, position = ?5, files = ?6, bytes = ?7, updated_at = ?8`,
      )
        .bind(id, body.title, body.icon, body.parent, body.position, JSON.stringify(body.files), body.html.length, existing?.created_at ?? now)
        .run();
      await pruneFiles(env, id, new Set(body.files));
      return json({ id, url: pageUrl(env, request, body.title, id), missing: await missingFiles(env, id, body.files) }, existing ? 200 : 201);
    }
    if (method === "DELETE") {
      await env.DB.prepare("DELETE FROM pages WHERE id = ?").bind(id).run();
      await env.FILES.delete(htmlKey(id));
      await pruneFiles(env, id, new Set());
      return new Response(null, { status: 204 });
    }
  }
  return fail(404, "There is no such call.");
}

/** A page's file, for anyone: names are content hashes, so one never changes. */
async function servedFile(request: Request, env: Env, id: string, name: string): Promise<Response> {
  if (!PAGE_ID.test(id) || !FILE_NAME.test(name)) return new Response("Not found", { status: 404 });
  const object = await env.FILES.get(filesKey(id, name), { range: request.headers, onlyIf: request.headers });
  if (!object) return new Response("Not found", { status: 404 });
  const headers = new Headers();
  object.writeHttpMetadata(headers);
  headers.set("etag", object.httpEtag);
  headers.set("accept-ranges", "bytes");
  headers.set("cache-control", "public, max-age=31536000, immutable");
  headers.set("x-content-type-options", "nosniff");
  headers.set("x-robots-tag", "noindex");
  // Pictures and the like open in the page; anything that could run script is downloaded instead.
  if (/html|svg|xml|javascript/i.test(headers.get("content-type") ?? "")) {
    headers.set("content-security-policy", "sandbox");
    headers.set("content-disposition", "attachment");
  }
  if (!("body" in object)) return new Response(null, { status: 304, headers });
  const range = object.range as { offset?: number; length?: number } | undefined;
  if (range && request.headers.has("range")) {
    const offset = range.offset ?? 0;
    const length = range.length ?? object.size - offset;
    headers.set("content-range", `bytes ${offset}-${offset + length - 1}/${object.size}`);
    headers.set("content-length", String(length));
    return new Response(object.body, { status: 206, headers });
  }
  headers.set("content-length", String(object.size));
  return new Response(object.body, { headers });
}

async function crumbsOf(env: Env, request: Request, row: PageRow): Promise<Crumb[]> {
  const crumbs: Crumb[] = [];
  const seen = new Set([row.id]);
  let parent = row.parent_id;
  while (parent && crumbs.length < 8 && !seen.has(parent)) {
    seen.add(parent);
    const above = await env.DB.prepare("SELECT * FROM pages WHERE id = ?").bind(parent).first<PageRow>();
    if (!above) break;
    crumbs.unshift({ title: above.title, href: `/${encodeURI(canonicalPath(above.title, above.id))}` });
    parent = above.parent_id;
  }
  return crumbs;
}

async function publicPage(request: Request, env: Env, segment: string): Promise<Response> {
  const notFound = () => new Response(NOT_FOUND_HTML, { status: 404, headers: PAGE_HEADERS });
  let wanted: string;
  try {
    wanted = decodeURIComponent(segment);
  } catch {
    return notFound();
  }
  const id = idOfPath(wanted);
  if (!PAGE_ID.test(id)) return notFound();
  const row = await env.DB.prepare("SELECT * FROM pages WHERE id = ?").bind(id).first<PageRow>();
  const object = row ? await env.FILES.get(htmlKey(id)) : null;
  if (!row || !object) return notFound();
  // One address per page: a changed title, or a bare id, goes to the current one.
  const canonical = canonicalPath(row.title, id);
  if (wanted !== canonical) return new Response(null, { status: 301, headers: { location: `/${encodeURI(canonical)}`, "x-robots-tag": "noindex" } });
  const html = renderPage({ env, request, row, html: await object.text(), crumbs: await crumbsOf(env, request, row) });
  return new Response(request.method === "HEAD" ? null : html, { headers: PAGE_HEADERS });
}

export default {
  async fetch(request: Request, env: Env): Promise<Response> {
    const url = new URL(request.url);
    const { pathname } = url;
    if (pathname.startsWith("/api/v1/")) return api(request, env, url);
    if (request.method !== "GET" && request.method !== "HEAD") return new Response("Method not allowed", { status: 405 });
    if (pathname === "/") return new Response(HOME_HTML, { headers: PAGE_HEADERS });
    if (pathname === "/robots.txt") return new Response("User-agent: *\nDisallow: /\n", { headers: { "content-type": "text/plain" } });
    if (pathname.startsWith("/katex/")) return env.ASSETS.fetch(request);
    const file = /^\/f\/([^/]+)\/([^/]+)$/.exec(pathname);
    if (file) return servedFile(request, env, file[1], file[2]);
    if (/^\/[^/]+$/.test(pathname)) return publicPage(request, env, pathname.slice(1));
    return new Response(NOT_FOUND_HTML, { status: 404, headers: PAGE_HEADERS });
  },
} satisfies ExportedHandler<Env>;
