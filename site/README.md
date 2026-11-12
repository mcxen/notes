# The landing page

A static page: `index.html` and `assets/`, no build step beyond `latest.json`.

## Deploying on Cloudflare Pages (notes.xpai.uk)

1. Cloudflare dashboard › Workers & Pages › Create › Pages › Connect to Git, choose this repository.
2. Build settings: **Build command** `node site/build.mjs`, **Build output directory** `site`, no root directory. (Production branch `main`.)
3. After the first deploy the page is at `https://<project>.pages.dev`.
4. Project › Custom domains › Set up a domain › `notes.xpai.uk`. When `xpai.uk` is a Cloudflare zone the DNS record is added for you; otherwise add the `CNAME notes → <project>.pages.dev` it shows.

## How the page finds the newest release

`build.mjs` writes `latest.json` (the newest release's files) at deploy time, so a visitor's browser need not call GitHub's API, which is slow or blocked in some countries. The page reads `latest.json` first on the Chinese page and GitHub's API first on the English one, and falls back to the other, then to fixed v0.1.3 addresses. `latest.json` is as new as the last deploy: redeploy after a release (a Pages Deploy Hook called from the release workflow does it).

## Downloads and images

- Chinese (简体中文) visitors get the `tvv.tw` mirror for the installers, with a link to the release's `SHA256SUMS`; English visitors get GitHub directly.
- The screenshots are served from jsDelivr (`cdn.jsdelivr.net/gh/mcxen/notes@main/site/assets/`, WebP) and fall back to this site's own copies.
