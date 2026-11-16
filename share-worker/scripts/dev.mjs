// A local worker for development: wrangler dev with D1 and R2 on disk (in
// .wrangler/state), the migrations applied, and a token of your choice.
//
//   npm run dev                          http://127.0.0.1:8787, token "local-dev-token"
//   npm run dev -- --port 8795 --token x
import { execFileSync, spawn } from "node:child_process";
import { defaults, root, writeWranglerConfig } from "./config.mjs";

const args = process.argv.slice(2);
const option = (name, fallback) => {
  const at = args.indexOf(`--${name}`);
  return at >= 0 && args[at + 1] ? args[at + 1] : fallback;
};
const port = option("port", "8787");
const token = option("token", "local-dev-token");

execFileSync("node", ["scripts/assets.mjs"], { cwd: root, stdio: "inherit" });
writeWranglerConfig({ ...defaults, customDomain: "" }, { local: true });
execFileSync("npx", ["--no-install", "wrangler", "d1", "migrations", "apply", defaults.databaseName, "--local"], { cwd: root, stdio: ["ignore", "inherit", "inherit"] });
console.log(`\nNotes Share on http://127.0.0.1:${port}  (token: ${token})\n`);
spawn("npx", ["--no-install", "wrangler", "dev", "--local", "--port", port, "--var", `NOTES_TOKEN:${token}`], { cwd: root, stdio: "inherit" }).on("exit", (code) => process.exit(code ?? 0));
