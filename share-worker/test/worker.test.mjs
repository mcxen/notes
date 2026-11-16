// The worker against `wrangler dev --local`: a real Workers runtime with D1 and
// R2 on disk, the same protocol the Notes app speaks.
import assert from "node:assert/strict";
import { spawn, execFileSync } from "node:child_process";
import { mkdtempSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, describe, it } from "node:test";
import { defaults, root, wranglerConfig } from "../scripts/config.mjs";

const PORT = 8791;
const BASE = `http://127.0.0.1:${PORT}`;
const TOKEN = "test-token-1234567890";
const PUBLIC = "https://share.example.test";

let child;
let log = "";
let persist;
const config = join(root, "wrangler.test.jsonc");

const call = (path, { method = "GET", body, token = TOKEN, type = "application/json", raw } = {}) =>
  fetch(`${BASE}${path}`, {
    method,
    redirect: "manual",
    headers: { ...(token ? { authorization: `Bearer ${token}` } : {}), ...(body !== undefined || raw ? { "content-type": type } : {}) },
    body: raw ?? (body === undefined ? undefined : JSON.stringify(body)),
  });

const put = (id, body) => call(`/api/v1/pages/${id}`, { method: "PUT", body });

before(async () => {
  persist = mkdtempSync(join(tmpdir(), "notes-share-test-"));
  const deployment = { ...defaults, customDomain: "" };
  const cfg = wranglerConfig(deployment, { local: true });
  cfg.vars.PUBLIC_URL = PUBLIC;
  writeFileSync(config, JSON.stringify(cfg));
  execFileSync("npx", ["--no-install", "wrangler", "d1", "migrations", "apply", "notes-share-db", "--local", "--config", config, "--persist-to", persist], { cwd: root, stdio: "ignore" });
  child = spawn("npx", ["--no-install", "wrangler", "dev", "--local", "--config", config, "--port", String(PORT), "--persist-to", persist, "--var", `NOTES_TOKEN:${TOKEN}`], { cwd: root, stdio: ["ignore", "pipe", "pipe"], detached: true });
  child.stdout.on("data", (chunk) => (log += chunk));
  child.stderr.on("data", (chunk) => (log += chunk));
  for (let i = 0; i < 120; i++) {
    try {
      if ((await call("/api/v1/info")).ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
  throw new Error("The worker did not start.");
});

after(() => {
  if (process.env.SHARE_DEBUG) console.error(log);
  // npx starts wrangler, which starts the runtime: stop the whole group.
  if (child?.pid) try { process.kill(-child.pid, "SIGKILL"); } catch {}
  rmSync(persist, { recursive: true, force: true });
  rmSync(config, { force: true });
});

describe("the worker", () => {
  it("says what it is, and that its database is up to date", async () => {
    const info = await (await call("/api/v1/info")).json();
    assert.equal(info.app, "notes-share");
    assert.equal(info.apiVersion, 1);
    assert.deepEqual(info.migrations, { applied: 1, bundled: 1, pending: 0 });
    assert.equal(readdirSync(join(root, "migrations")).filter((n) => n.endsWith(".sql")).length, info.migrations.bundled);
  });

  it("refuses a call without the token, or with a wrong one", async () => {
    assert.equal((await call("/api/v1/info", { token: null })).status, 401);
    assert.equal((await call("/api/v1/info", { token: "nope" })).status, 401);
    assert.equal((await call("/api/v1/pages")).status, 200);
  });

  it("publishes a page, serves it at a readable address and sends the old one to it", async () => {
    const reply = await put("aaaaaaaaaaaa", { title: "Road map", html: "<p>Hello <b>world</b></p>", files: [] });
    assert.equal(reply.status, 201);
    const made = await reply.json();
    assert.equal(made.url, `${PUBLIC}/road-map-aaaaaaaaaaaa`);
    assert.deepEqual(made.missing, []);

    const page = await call("/road-map-aaaaaaaaaaaa");
    assert.equal(page.status, 200);
    const html = await page.text();
    assert.match(html, /<title>Road map<\/title>/);
    assert.match(html, /<p>Hello <b>world<\/b><\/p>/);
    assert.match(html, /noindex/);
    assert.match(page.headers.get("content-security-policy"), /default-src 'none'/);
    assert.equal(page.headers.get("x-robots-tag"), "noindex, nofollow");

    // A bare id, and an old title, go to the current address.
    for (const old of ["/aaaaaaaaaaaa", "/old-name-aaaaaaaaaaaa"]) {
      const moved = await call(old);
      assert.equal(moved.status, 301);
      assert.equal(moved.headers.get("location"), "/road-map-aaaaaaaaaaaa");
    }
  });

  it("updates in place and keeps one address when the page is published again", async () => {
    const again = await put("aaaaaaaaaaaa", { title: "Road map", html: "<p>Second</p>", files: [] });
    assert.equal(again.status, 200);
    assert.match(await (await call("/road-map-aaaaaaaaaaaa")).text(), /Second/);
  });

  it("asks for the files it lacks, takes them once, and serves them for good", async () => {
    const name = "0123456789ab.png";
    const reply = await (await put("bbbbbbbbbbbb", { title: "With a picture", html: `<img src="/f/bbbbbbbbbbbb/${name}">`, files: [name] })).json();
    assert.deepEqual(reply.missing, [name]);

    const bytes = new Uint8Array([137, 80, 78, 71, 13, 10, 26, 10, 1, 2, 3, 4]);
    const up = await call(`/api/v1/pages/bbbbbbbbbbbb/files/${name}`, { method: "PUT", raw: bytes, type: "image/png" });
    assert.equal(up.status, 201);

    const again = await (await put("bbbbbbbbbbbb", { title: "With a picture", html: "<p>x</p>", files: [name] })).json();
    assert.deepEqual(again.missing, []);

    const file = await call(`/f/bbbbbbbbbbbb/${name}`);
    assert.equal(file.status, 200);
    assert.equal(file.headers.get("content-type"), "image/png");
    assert.match(file.headers.get("cache-control"), /immutable/);
    assert.deepEqual(new Uint8Array(await file.arrayBuffer()), bytes);

    const range = await call(`/f/bbbbbbbbbbbb/${name}`, { token: null });
    assert.equal(range.status, 200);
  });

  it("refuses a file the page does not list, and a file for a page that is not there", async () => {
    assert.equal((await call("/api/v1/pages/bbbbbbbbbbbb/files/other.png", { method: "PUT", raw: new Uint8Array([1]), type: "image/png" })).status, 409);
    assert.equal((await call("/api/v1/pages/zzzzzzzzzzzz/files/a.png", { method: "PUT", raw: new Uint8Array([1]), type: "image/png" })).status, 404);
  });

  it("prunes the files a page no longer lists", async () => {
    await put("bbbbbbbbbbbb", { title: "With a picture", html: "<p>x</p>", files: [] });
    assert.equal((await call("/f/bbbbbbbbbbbb/0123456789ab.png")).status, 404);
  });

  it("serves a script-capable file as a download, not a page", async () => {
    await put("cccccccccccc", { title: "Svg", html: "<p>x</p>", files: ["x.svg"] });
    await call("/api/v1/pages/cccccccccccc/files/x.svg", { method: "PUT", raw: "<svg onload=alert(1)/>", type: "image/svg+xml" });
    const file = await call("/f/cccccccccccc/x.svg");
    assert.equal(file.headers.get("content-disposition"), "attachment");
    assert.equal(file.headers.get("content-security-policy"), "sandbox");
  });

  it("shows a page's parents above it", async () => {
    await put("dddddddddddd", { title: "Parent", html: "<p>p</p>", files: [] });
    await put("eeeeeeeeeeee", { title: "Child", html: "<p>c</p>", parent: "dddddddddddd", files: [] });
    const html = await (await call("/child-eeeeeeeeeeee")).text();
    assert.match(html, /<nav class="crumbs"[^>]*><a href="\/parent-dddddddddddd">Parent<\/a>/);
  });

  it("lists what is published, with each page's address", async () => {
    const { pages } = await (await call("/api/v1/pages")).json();
    const child = pages.find((p) => p.id === "eeeeeeeeeeee");
    assert.equal(child.parent, "dddddddddddd");
    assert.equal(child.url, `${PUBLIC}/child-eeeeeeeeeeee`);
  });

  it("refuses a bad page without writing anything", async () => {
    assert.equal((await put("ffffffffffff", { title: "", html: "x" })).status, 400);
    assert.equal((await put("ffffffffffff", { title: "T", html: "x", files: ["../etc"] })).status, 400);
    assert.equal((await put("ffffffffffff", { title: "T", html: "x", parent: "ffffffffffff" })).status, 400);
    assert.equal((await call("/api/v1/pages/ffffffffffff")).status, 404);
    assert.equal((await put("BAD!", { title: "T", html: "x" })).status, 400);
  });

  it("unpublishes a page and its files", async () => {
    assert.equal((await call("/api/v1/pages/cccccccccccc", { method: "DELETE" })).status, 204);
    assert.equal((await call("/cccccccccccc")).status, 404);
    assert.equal((await call("/f/cccccccccccc/x.svg")).status, 404);
  });

  it("answers a page that is not there, and tells search engines to stay away", async () => {
    const missing = await call("/nothing-zzzzzzzzzzzz");
    assert.equal(missing.status, 404);
    const robots = await (await call("/robots.txt")).text();
    assert.match(robots, /Disallow: \//);
  });

  it("links the equation stylesheet only for pages that have an equation", async () => {
    await put("gggggggggggg", { title: "Math", html: '<span class="katex">x</span>', files: [] });
    assert.match(await (await call("/math-gggggggggggg")).text(), /katex\.min\.css/);
    assert.equal((await call("/katex/katex.min.css")).status, 200);
    assert.doesNotMatch(await (await call("/road-map-aaaaaaaaaaaa")).text(), /katex\.min\.css/);
  });
});
