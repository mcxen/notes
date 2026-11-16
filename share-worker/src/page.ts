import type { Env } from "./env";

export interface PageRow {
  id: string;
  title: string;
  icon: string | null;
  parent_id: string | null;
  position: number;
  files: string;
  bytes: number;
  created_at: number;
  updated_at: number;
}

export function escapeHtml(text: string): string {
  return text.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

/** The readable part of a page's address: its title as letters, digits and hyphens. */
export function slugOf(title: string): string {
  return title
    .normalize("NFKC")
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 60)
    .replace(/-+$/g, "");
}

/** The address of a page below the site's root: `title-id`, or the id alone. */
export function canonicalPath(title: string, id: string): string {
  const slug = slugOf(title);
  return slug ? `${slug}-${id}` : id;
}

/** The page id in a path: what follows the last hyphen, or the whole of it. */
export function idOfPath(path: string): string {
  const at = path.lastIndexOf("-");
  return at < 0 ? path : path.slice(at + 1);
}

export function siteOrigin(env: Env, request: Request): string {
  return (env.PUBLIC_URL?.trim() || new URL(request.url).origin).replace(/\/+$/, "");
}

export function pageUrl(env: Env, request: Request, title: string, id: string): string {
  return `${siteOrigin(env, request)}/${encodeURI(canonicalPath(title, id))}`;
}

/** A sentence of the page's words, for the preview of its link. */
function excerpt(html: string): string {
  const text = html
    .replace(/<(script|style)[\s\S]*?<\/\1>/gi, " ")
    .replace(/<[^>]+>/g, " ")
    .replace(/&nbsp;/g, " ")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/\s+/g, " ")
    .trim();
  return text.length > 160 ? `${text.slice(0, 157)}…` : text;
}

const STYLE = `
:root{color-scheme:light dark;--bg:#fff;--ink:#37352f;--ink-2:#787774;--line:rgba(55,53,47,.16);--fill:rgba(135,131,120,.12);--link:#2383e2}
@media (prefers-color-scheme:dark){:root{--bg:#191919;--ink:rgba(255,255,255,.9);--ink-2:#9b9a97;--line:rgba(255,255,255,.14);--fill:rgba(255,255,255,.07);--link:#529cca}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--ink);font:16px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI","PingFang SC","Microsoft YaHei",sans-serif;-webkit-text-size-adjust:100%}
.crumbs{max-width:860px;margin:0 auto;padding:18px 24px 0;font-size:14px;color:var(--ink-2);display:flex;flex-wrap:wrap;gap:6px}
.crumbs a{color:var(--ink-2);text-decoration:none}.crumbs a:hover{text-decoration:underline}
main{max-width:860px;margin:0 auto;padding:32px 24px 72px}
h1.title{font-size:2.3em;line-height:1.2;margin:0 0 .5em}
h1.title .icon{margin-right:.25em}
h1,h2,h3{line-height:1.3}h2{margin:1.5em 0 .4em}h3{margin:1.3em 0 .3em}
p{margin:.4em 0}a{color:var(--link)}
img{max-width:100%;height:auto;border-radius:6px}figure{margin:1em 0}figcaption{color:var(--ink-2);font-size:.9em}
blockquote{margin:.7em 0;padding-left:1em;border-left:3px solid currentColor}
hr{border:0;border-top:1px solid var(--line);margin:1.5em 0}
code{font:.9em ui-monospace,SFMono-Regular,Menlo,monospace;background:var(--fill);padding:.1em .35em;border-radius:4px}
pre{background:var(--fill);padding:14px 16px;border-radius:8px;overflow:auto}pre code{background:none;padding:0}
li.todo{list-style:none;margin-left:-1.2em}
.callout{display:flex;gap:10px;background:var(--fill);padding:14px 16px;border-radius:8px;margin:.8em 0}
table{border-collapse:collapse;margin:1em 0;display:block;overflow-x:auto}td,th{border:1px solid var(--line);padding:6px 10px;text-align:left;vertical-align:top}
details>summary{cursor:pointer}
.columns{display:flex;gap:2em}.column{min-width:0}
@media (max-width:640px){.columns{flex-direction:column;gap:0}}
.page-link{margin:.3em 0}.page-link a{text-decoration:none;font-weight:500}.page-link a:hover{text-decoration:underline}
.diagram{display:flex;justify-content:center;margin:1em 0;padding:12px;background:#fff;border-radius:8px;overflow-x:auto}.diagram svg{max-width:100%;height:auto}
.math{overflow-x:auto;margin:.8em 0;text-align:center}
.db{margin:1em 0}.db th{background:var(--fill);font-weight:600}
.tag{display:inline-block;padding:0 8px;border-radius:4px;background:var(--fill);font-size:.9em;white-space:nowrap}
.tag-gray{background:rgba(120,119,116,.18)}.tag-brown{background:rgba(159,107,83,.2)}.tag-orange{background:rgba(217,115,13,.2)}.tag-yellow{background:rgba(203,145,47,.22)}
.tag-green{background:rgba(68,131,97,.22)}.tag-blue{background:rgba(51,126,169,.2)}.tag-purple{background:rgba(144,101,176,.2)}.tag-pink{background:rgba(193,76,138,.2)}.tag-red{background:rgba(212,76,71,.2)}
footer{max-width:860px;margin:0 auto;padding:0 24px 40px;font-size:13px;color:var(--ink-2)}
`;

const CSP = [
  "default-src 'none'",
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data: https:",
  "media-src 'self' https:",
  "font-src 'self' data:",
  "base-uri 'none'",
  "form-action 'none'",
  "frame-ancestors 'none'",
].join("; ");

export const PAGE_HEADERS = {
  "content-type": "text/html; charset=utf-8",
  "content-security-policy": CSP,
  "x-content-type-options": "nosniff",
  "x-robots-tag": "noindex, nofollow",
  "referrer-policy": "no-referrer",
  "cache-control": "no-cache",
};

export interface Crumb {
  title: string;
  href: string;
}

export function renderPage(opts: { env: Env; request: Request; row: PageRow; html: string; crumbs: Crumb[] }): string {
  const { row, html, crumbs, env, request } = opts;
  const origin = siteOrigin(env, request);
  const description = excerpt(html);
  const image = /<img\b[^>]*\bsrc="(\/f\/[^"]+)"/.exec(html)?.[1];
  const katex = html.includes('class="katex');
  const updated = new Date(row.updated_at * 1000).toISOString().slice(0, 10);
  const trail = crumbs.length
    ? `<nav class="crumbs" aria-label="Breadcrumb">${crumbs.map((c) => `<a href="${escapeHtml(c.href)}">${escapeHtml(c.title)}</a><span>/</span>`).join("")}<span>${escapeHtml(row.title)}</span></nav>`
    : "";
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex, nofollow">
<title>${escapeHtml(row.title)}</title>
<meta name="description" content="${escapeHtml(description)}">
<meta property="og:title" content="${escapeHtml(row.title)}">
<meta property="og:description" content="${escapeHtml(description)}">
<meta property="og:type" content="article">
${image ? `<meta property="og:image" content="${escapeHtml(origin + image)}">` : ""}
${katex ? '<link rel="stylesheet" href="/katex/katex.min.css">' : ""}
<style>${STYLE}</style>
</head>
<body>
${trail}
<main>
<h1 class="title">${row.icon ? `<span class="icon">${escapeHtml(row.icon)}</span>` : ""}${escapeHtml(row.title)}</h1>
<article>
${html}
</article>
</main>
<footer>Updated ${updated}</footer>
</body>
</html>
`;
}

export const NOT_FOUND_HTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex"><title>Not found</title><style>${STYLE}</style></head><body><main><h1 class="title">This page isn’t here</h1><p>It may have been unpublished, or the link is mistyped.</p></main></body></html>`;

export const HOME_HTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><meta name="robots" content="noindex"><title>Notes Share</title><style>${STYLE}</style></head><body><main><h1 class="title">Notes Share</h1><p>Pages published from Notes are served here.</p></main></body></html>`;
