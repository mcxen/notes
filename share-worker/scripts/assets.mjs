// Copies KaTeX's stylesheet and its woff2 fonts into public/katex, which the
// worker serves for pages with equations. Run by `npm run dev`, `npm test` and
// the deploy script; the copy is not committed.
import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const from = join(root, "node_modules", "katex", "dist");
const to = join(root, "public", "katex");

if (!existsSync(from)) {
  console.error("katex is not installed: run `npm install` first.");
  process.exit(1);
}
rmSync(to, { recursive: true, force: true });
mkdirSync(join(to, "fonts"), { recursive: true });
cpSync(join(from, "katex.min.css"), join(to, "katex.min.css"));
for (const name of readdirSync(join(from, "fonts"))) {
  if (name.endsWith(".woff2")) cpSync(join(from, "fonts", name), join(to, "fonts", name));
}
