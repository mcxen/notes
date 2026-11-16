// The wrangler config of a deployment. wrangler.jsonc is generated from
// share.deploy.json (your names and ids), so the template never holds them and
// pulling an update never conflicts with them.
import { existsSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
export const deployFile = join(root, "share.deploy.json");

export const defaults = {
  workerName: "notes-share",
  databaseName: "notes-share-db",
  databaseId: "",
  bucketName: "notes-share-files",
  accountId: "",
  // A host on a zone in your Cloudflare account, e.g. share.example.com; "" uses workers.dev.
  customDomain: "",
  maxFileMb: 25,
};

export function readDeployment() {
  return { ...defaults, ...(existsSync(deployFile) ? JSON.parse(readFileSync(deployFile, "utf8")) : {}) };
}

export function writeDeployment(deployment) {
  writeFileSync(deployFile, `${JSON.stringify(deployment, null, 2)}\n`);
}

/** The wrangler config for a deployment; `local` has no ids, for `wrangler dev`. */
export function wranglerConfig(deployment, { local = false } = {}) {
  const config = {
    $schema: "node_modules/wrangler/config-schema.json",
    name: deployment.workerName,
    main: "src/index.ts",
    compatibility_date: "2026-07-01",
    workers_dev: !deployment.customDomain,
    d1_databases: [
      { binding: "DB", database_name: deployment.databaseName, database_id: local ? "00000000-0000-0000-0000-000000000000" : deployment.databaseId, migrations_dir: "migrations" },
    ],
    r2_buckets: [{ binding: "FILES", bucket_name: deployment.bucketName }],
    assets: { directory: "public", binding: "ASSETS", run_worker_first: true },
    vars: { MAX_FILE_MB: String(deployment.maxFileMb) },
    observability: { enabled: true },
  };
  if (deployment.customDomain) {
    config.routes = [{ pattern: deployment.customDomain, custom_domain: true }];
    config.vars.PUBLIC_URL = `https://${deployment.customDomain}`;
  }
  return config;
}

export function writeWranglerConfig(deployment, options) {
  writeFileSync(join(root, "wrangler.jsonc"), `${JSON.stringify(wranglerConfig(deployment, options), null, 2)}\n`);
}
