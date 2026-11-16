# Deploying Notes Share

These are the steps for a coding agent (Claude Code, Codex, OpenCode, …) setting
up Notes Share on its user's own Cloudflare account, or updating a copy that is
already deployed. Follow them in order. Each step says what to do if something
goes wrong.

Notes Share is a Cloudflare Worker that serves the pages published from the
[Notes](https://github.com/mcxen/notes) app. Published pages go in an R2 bucket
(their HTML and their images and files), with a D1 database as the index. The
app talks to the worker with one shared token, kept as a Worker secret.

Everything runs on the **user's** account. Nothing here belongs to anyone else,
and no address or account is built in: the user chooses them.

## 0. New setup or update?

- If `share.deploy.json` exists in this folder, this is an **update**: go to
  [Updating](#updating).
- Otherwise it is a **new setup**: start at step 1.

## 1. Get the code

Requirements: `git`, and Node.js 20 or newer.

This folder is `share-worker/` of <https://github.com/mcxen/notes>. If you are not
already in a copy of it, look for one on this computer first; otherwise:

```bash
git clone https://github.com/mcxen/notes.git
cd notes/share-worker
```

## 2. Install

```bash
npm install
```

## 3. Sign in to Cloudflare

```bash
npx wrangler whoami
```

If it does not say you are logged in, ask the user to run `npx wrangler login`
themselves (it opens a browser), then check again. If they have no Cloudflare
account yet, the sign-in page lets them make one, free.

## 4. Ask the user two things

1. **Which Cloudflare account**, if `whoami` lists more than one. Pass its id as
   `--account <id>` in step 5. It is remembered after that.
2. **Whether to use their own domain**, such as `share.example.com`: a host on a
   domain that is already in this Cloudflare account. If not, the worker is
   reached at a `workers.dev` address and nothing more is needed. If so, pass
   `--domain <host>` in step 5. Do not guess a domain.

## 5. Deploy

```bash
npm run deploy
```

with `-- --account <id>` and `-- --domain <host>` added as the user chose, for example
`npm run deploy -- --domain share.example.com`. It does everything, and is safe to
run again:

- creates the D1 database and the R2 bucket, or reuses them
- writes their names and ids to `share.deploy.json`
- applies the database migrations
- makes a random token and saves it as the Worker secret `NOTES_TOKEN`
- deploys the worker and prints its address and the token

Options: `--name <worker-name>` on the first run only, to call the worker something
other than `notes-share` (it becomes part of the `workers.dev` address);
`--rotate-token` to make a new token (the old one stops working).

If a step fails, read the error. Most failures are one of these:

| Error | What to do |
| --- | --- |
| Not signed in | Step 3. |
| More than one account | Ask the user which, then pass `--account`. |
| R2 is not enabled | The user opens the Cloudflare dashboard → R2 Object Storage and turns it on (it has a free tier and asks for a payment method), then run again. |
| A `workers.dev` subdomain is not set up | The user opens Workers & Pages in the dashboard once, which sets one up, then run again. |
| A name is taken | Run again with `-- --name <another-name>` (first run only), or edit the names in `share.deploy.json`. |
| The domain is not in this account | The domain must be a zone of this Cloudflare account. Use another host, or add the domain to the account first. |

## 6. Hand over the address and the token

The script ends by printing:

```
  Worker URL  https://…
  Token       <a long random string>
```

Give both to the user and tell them:

> In Notes, open **Settings → Cloud**, turn on publishing, paste the worker's
> address and the token, and press **Check**.

The token is shown only once and is not saved on disk. Do not write it to a file,
a commit or a log. If it is lost, make a new one with
`npm run deploy -- --rotate-token`; the old one stops working.

## Updating

When Notes says the worker is out of date, or the user asks to update:

```bash
git pull
npm install
npm run deploy
```

It applies new migrations and redeploys. It keeps the existing token, so Notes
stays connected and there is nothing to paste again.

## What is deployed

| Resource | Default name | Holds |
| --- | --- | --- |
| Worker | `notes-share` | the API and the published pages |
| D1 database | `notes-share-db` | the index of published pages |
| R2 bucket | `notes-share-files` | their HTML, images and files |
| Secret | `NOTES_TOKEN` | the shared token |

All of it fits the Cloudflare free plan for personal use. To remove everything,
delete those from the Cloudflare dashboard.

## What the worker does and does not do

- A page is at `/<title>-<id>`; `/<id>` and an old title send the visitor to the
  current address. The id is twelve random characters, so a page is not found by
  guessing, but **anyone with the link can read it**: it is unlisted, not private.
- Every page and file is sent with `noindex` headers and `robots.txt` disallows
  everything. Pages are served with a strict Content Security Policy: no script
  runs in them.
- `PUBLIC_URL` (set by `--domain`) is the address the app is told for a page,
  whichever address it uses to reach the worker.
- A file over `MAX_FILE_MB` (25 by default; the app has its own limit too) is
  refused.
