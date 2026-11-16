package share

import "strings"

// TemplateRepository is where the worker's code is, and TemplatePath the folder in it.
const (
	TemplateRepository = "https://github.com/mcxen/notes"
	TemplatePath       = "share-worker"
)

// SetupPrompt is what the user pastes into their coding agent. It is short and
// points at DEPLOY.md, so the steps can change without a new Notes. The worker's
// address and the Cloudflare account and domain are the user's to choose: it
// says to ask.
func SetupPrompt() string {
	return strings.TrimSpace(`
Set up Notes Share on my Cloudflare account, so the Notes app can publish my pages to the web. If I already have it set up, update it instead.

The code is the `+TemplatePath+`/ folder of `+TemplateRepository+`. If you are not already in a copy of it, look for one on this computer first, otherwise clone the repository.

Follow `+TemplatePath+`/DEPLOY.md step by step. It covers installing, signing in to Cloudflare, deploying a worker with a D1 database and an R2 bucket, and updating one that is already deployed.

Ask me which Cloudflare account to use if there is more than one, and whether I want the pages on my own domain (a host on a zone in my account) or on the default workers.dev address.

When you are done, give me the Worker URL and the token, so I can paste them into Notes > Settings > Cloud. Do not save the token in any file.
`) + "\n"
}
