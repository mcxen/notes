# Notes Share

The worker the [Notes](https://github.com/mcxen/notes) app publishes pages to, on
**your own Cloudflare account**: a Worker, a D1 database and an R2 bucket. Notes
runs no server for this; nothing is sent anywhere until you turn on publishing in
Settings → Cloud and press Publish.

To set it up, copy the setup prompt from Notes → Settings → Cloud into your coding
agent, or follow [DEPLOY.md](DEPLOY.md) yourself.

## Develop

```bash
npm install
npm run dev        # wrangler dev --local, with D1 and R2 on disk
npm test           # the worker against `wrangler dev --local`
npm run typecheck
```

## The API (version 1)

All calls but the public pages need `Authorization: Bearer <token>`.

| Call | |
| --- | --- |
| `GET /api/v1/info` | `{app, apiVersion, version, migrations: {applied, bundled, pending}, pages}` |
| `GET /api/v1/pages` | the published pages |
| `PUT /api/v1/pages/:id` | `{title, html, icon?, parent?, position?, files[]}` → `{id, url, missing[]}` |
| `PUT /api/v1/pages/:id/files/:name` | the raw bytes of one of the page's listed files |
| `DELETE /api/v1/pages/:id` | the page and its files |
| `GET /<title>-<id>`, `GET /f/<id>/<name>` | public |

`id` is chosen by the app (lowercase letters and digits), so publishing again
updates in place. A file is named by a hash of its bytes, so one that did not change
is never sent twice. The design follows
[Lotion Share](https://github.com/fayazara/lotion-share) (MIT).
