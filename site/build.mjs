// Writes latest.json: the newest release's tag, page and files, from GitHub's API,
// so the page can find the downloads without calling GitHub from the visitor's
// browser (slow or blocked in some countries). Run at deploy time:
//   Cloudflare Pages: Build command `node site/build.mjs`, Build output directory `site`.
// A failure keeps the file that is there, so a deploy never fails for it.
import { readFile, writeFile } from "node:fs/promises";

const file = new URL("./latest.json", import.meta.url);
try {
  const headers = { Accept: "application/vnd.github+json", "User-Agent": "notes-site-build" };
  if (process.env.GITHUB_TOKEN) headers.Authorization = `Bearer ${process.env.GITHUB_TOKEN}`;
  const response = await fetch("https://api.github.com/repos/mcxen/notes/releases/latest", { headers });
  if (!response.ok) throw new Error(`GitHub answered ${response.status}`);
  const release = await response.json();
  const slim = {
    tag_name: release.tag_name,
    html_url: release.html_url,
    published_at: release.published_at,
    assets: (release.assets ?? []).map(({ name, browser_download_url }) => ({ name, browser_download_url })),
  };
  await writeFile(file, JSON.stringify(slim, null, 2) + "\n");
  console.log(`latest.json: ${slim.tag_name}, ${slim.assets.length} files`);
} catch (error) {
  const kept = await readFile(file, "utf8").then((text) => JSON.parse(text).tag_name, () => "none");
  console.warn(`latest.json not updated (${error.message}); keeping ${kept}`);
}
