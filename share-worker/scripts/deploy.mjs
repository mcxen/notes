// Deploys the worker to your own Cloudflare account, or updates a deployment.
// Safe to run again: it reuses what exists, applies new migrations, keeps the
// token, and prints the worker's address.
//
//   npm run deploy                       first run, or an update
//   npm run deploy -- --name <worker>    first run: another worker name
//   npm run deploy -- --domain <host>    serve from your own domain (a zone in this account)
//   npm run deploy -- --account <id>     the login has more than one account
//   npm run deploy -- --rotate-token     make a new token (the old one stops working)
//
// The token is shown once and is never written to disk.
import { execFileSync } from "node:child_process";
import { randomBytes } from "node:crypto";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readDeployment, root, writeDeployment, writeWranglerConfig } from "./config.mjs";

const args = process.argv.slice(2);
const option = (name) => {
  const at = args.indexOf(`--${name}`);
  if (at < 0) return undefined;
  const next = args[at + 1];
  return next && !next.startsWith("--") ? next : true;
};
const fail = (message) => {
  console.error(`\n${message}\n`);
  process.exit(1);
};

const env = { ...process.env };
const wrangler = (list, { allowFailure = false } = {}) => {
  try {
    return execFileSync("npx", ["--no-install", "wrangler", ...list], { cwd: root, env, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
  } catch (error) {
    if (allowFailure) return `${error.stdout ?? ""}${error.stderr ?? ""}`;
    fail(`wrangler ${list.join(" ")} failed:\n${error.stdout ?? ""}${error.stderr ?? ""}`);
  }
};
const step = (text) => console.log(`\n→ ${text}`);

execFileSync("node", [join(root, "scripts", "assets.mjs")], { stdio: "inherit" });

// 1. Signed in, and which account.
step("Checking the Cloudflare login");
let who;
try {
  who = JSON.parse(wrangler(["whoami", "--json"]));
} catch {
  fail("Could not read the Cloudflare login. Run `npx wrangler login` (it opens a browser), then try again.");
}
if (!who.loggedIn) fail("Not signed in to Cloudflare. Run `npx wrangler login` (it opens a browser), then try again.");
const deployment = readDeployment();
if (option("name") && !deployment.databaseId) deployment.workerName = option("name");
if (typeof option("domain") === "string") deployment.customDomain = option("domain");
if (typeof option("account") === "string") deployment.accountId = option("account");
const accounts = who.accounts ?? [];
if (!deployment.accountId) {
  if (accounts.length > 1) {
    fail(`This login has ${accounts.length} Cloudflare accounts. Ask which one to use, then run again with --account <id>:\n${accounts.map((a) => `  ${a.id}  ${a.name}`).join("\n")}`);
  }
  deployment.accountId = accounts[0]?.id ?? "";
}
if (!deployment.accountId) fail("No Cloudflare account found for this login.");
env.CLOUDFLARE_ACCOUNT_ID = deployment.accountId;

// 2. The database and the bucket.
step(`D1 database ${deployment.databaseName}`);
const findDatabase = () => JSON.parse(wrangler(["d1", "list", "--json"])).find((d) => d.name === deployment.databaseName);
let database = findDatabase();
if (!database) {
  wrangler(["d1", "create", deployment.databaseName]);
  database = findDatabase();
}
if (!database) fail("The D1 database was created but could not be found.");
deployment.databaseId = database.uuid;

step(`R2 bucket ${deployment.bucketName}`);
const created = wrangler(["r2", "bucket", "create", deployment.bucketName], { allowFailure: true });
if (/10042|enable R2/i.test(created)) {
  fail("R2 is not enabled on this Cloudflare account. Open the Cloudflare dashboard → R2 Object Storage, turn it on (it has a free tier, and asks for a payment method), then run this again.");
}
if (!/created|already exists|10004/i.test(created)) fail(`The R2 bucket could not be made:\n${created}`);

writeDeployment(deployment);
writeWranglerConfig(deployment);

// 3. Migrations.
step("Applying database migrations");
wrangler(["d1", "migrations", "apply", deployment.databaseName, "--remote"]);

// 4. Token and deploy.
const secrets = wrangler(["secret", "list", "--name", deployment.workerName], { allowFailure: true });
const hasToken = secrets.includes("NOTES_TOKEN");
const rotate = option("rotate-token") === true;
const token = rotate || !hasToken ? randomBytes(32).toString("base64url") : null;
const deployArgs = ["deploy"];
let dir;
if (token) {
  dir = mkdtempSync(join(tmpdir(), "notes-share-"));
  const file = join(dir, "secrets.json");
  writeFileSync(file, JSON.stringify({ NOTES_TOKEN: token }), { mode: 0o600 });
  deployArgs.push("--secrets-file", file);
}
step("Deploying the worker");
let deployed;
try {
  deployed = wrangler(deployArgs);
} finally {
  if (dir) rmSync(dir, { recursive: true, force: true });
}
const workersDev = /https:\/\/[A-Za-z0-9.-]+\.workers\.dev/.exec(deployed)?.[0];
const address = deployment.customDomain ? `https://${deployment.customDomain}` : workersDev;
if (!address) fail(`The worker is deployed, but its address could not be read from:\n${deployed}`);

console.log(`\nDone.\n\n  Worker URL  ${address}`);
console.log(token ? `  Token       ${token}\n\nIn Notes: Settings → Cloud → paste the URL and the token → Verify.\nThe token is shown only once. Do not save it in a file.` : "  Token       unchanged (use --rotate-token for a new one)");
