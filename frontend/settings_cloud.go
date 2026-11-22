package frontend

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/netproxy"
	"github.com/egoist/mygo/ui"

	"notes/internal/settings"
	"notes/internal/share"
)

// cloudState is what the last check of the worker found, for Settings › Cloud.
type cloudState struct {
	busy  string
	ok    string
	fail  string
	pages []share.Published
}

// cloudClient is a client of the worker as it is entered now.
func (s *settingsWindow) cloudClient() (*share.Client, error) {
	return share.New(s.st.Share.URL, s.st.Share.Token, netproxy.Transport())
}

// verifyCloud asks the worker who it is and what it holds, and says so on the page.
func (s *settingsWindow) verifyCloud() {
	client, err := s.cloudClient()
	if err != nil {
		s.cloud = cloudState{fail: err.Error()}
		return
	}
	s.cloud = cloudState{busy: tr("Checking the worker…")}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := client.Compatible(ctx)
		var pages []share.Published
		if err == nil {
			pages, err = client.List(ctx)
		}
		s.update(func() {
			if err != nil {
				s.cloud = cloudState{fail: err.Error()}
				return
			}
			s.cloud = cloudState{ok: tr("Connected: version %s, %d page(s) published.", info.Version, info.Pages), pages: pages}
		})
	}()
}

// unpublishCloud takes a page off the worker, and forgets where it was in every vault.
func (s *settingsWindow) unpublishCloud(id string) {
	client, err := s.cloudClient()
	if err != nil {
		s.cloud.fail = err.Error()
		return
	}
	s.cloud.busy = tr("Taking the page off the web…")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := client.Unpublish(ctx, id)
		if err == nil {
			err = shareStore().ForgetID(id)
		}
		s.update(func() {
			s.cloud.busy = ""
			if err != nil {
				s.cloud.fail = err.Error()
				return
			}
			s.cloud.ok = tr("The page is off the web.")
			var left []share.Published
			for _, p := range s.cloud.pages {
				if p.ID != id {
					left = append(left, p)
				}
			}
			s.cloud.pages = left
		})
	}()
}

// textField is a one-line input in a settings row; it reports Return.
func textField(c *ui.Context, pal *palette, key, label, placeholder string, value *string, width float32, secret bool) bool {
	var in ui.Element
	frame := ui.Row(c.Key(key + "-frame")).Width(width).Cursor(ui.CursorText)
	frame.Children(func() {
		in = ui.TextInputBase(c.Key(key), value).Grow(1).MinWidth(0).FontSize(13).Placeholder(placeholder).PlaceholderColor(pal.textFaint).Label(label)
		if secret {
			in.Password()
		}
	})
	field(frame, in.Focused(), pal)
	return in.Submitted()
}

func (s *settingsWindow) cloudPage(c *ui.Context, pal *palette) {
	sh := s.st.Share
	s.group(c, pal, tr("Publish to the web"), tr("Notes runs no server for this: you put a small worker on your own Cloudflare account, and a page you publish is kept there, at an address of yours. Off by default; while it is off Notes asks no worker for anything."), func() {
		on := sh.Enabled
		s.row(c, pal, tr("Turn on publishing"), tr("Adds Publish to the web to a page's menu."), func() {
			toggle(c, "cloud-on", &on, tr("Turn on publishing"), pal)
		})
		if on != sh.Enabled {
			s.st.Share.Enabled = on
			s.save()
			s.refresh()
		}
		s.row(c, pal, tr("Worker address"), tr("Your worker's address, on your own domain or workers.dev. Press Return to save."), func() {
			if textField(c, pal, "cloud-url", tr("Worker address"), "https://share.example.com", &s.cloudURL, 250, false) {
				s.st.Share.URL = strings.TrimSpace(s.cloudURL)
				s.save()
				s.refresh()
				s.cloud = cloudState{}
			}
		})
		tokenNote := tr("The secret your worker checks. Press Return to save.")
		if s.st.Share.Token != "" {
			tokenNote = tr("Saved: %s. Paste another and press Return to replace it.", maskedKey(s.st.Share.Token))
		}
		s.row(c, pal, tr("Token"), tokenNote, func() {
			if textField(c, pal, "cloud-token", tr("Token"), tr("Paste the token"), &s.cloudToken, 250, true) {
				s.st.Share.Token = strings.TrimSpace(s.cloudToken)
				s.cloudToken = ""
				s.save()
				s.refresh()
				s.cloud = cloudState{}
			}
		})
		s.row(c, pal, tr("Largest file"), tr("Megabytes. A bigger picture or file is left out of a published page. Press Return to save."), func() {
			if textField(c, pal, "cloud-mb", tr("Largest file"), "25", &s.cloudMB, 96, false) {
				mb, err := strconv.Atoi(strings.TrimSpace(s.cloudMB))
				if err != nil || mb < 1 || mb > 100 {
					s.notes = tr("The limit must be a number from 1 to 100.")
					return
				}
				s.st.Share.MaxFileMB = mb
				if mb == settings.DefaultShareFileMB {
					s.st.Share.MaxFileMB = 0
				}
				s.save()
				s.refresh()
			}
		})
		detail, tone := tr("Asks the worker for its version and pages."), pal.textMuted
		switch {
		case s.cloud.busy != "":
			detail = s.cloud.busy
		case s.cloud.fail != "":
			detail, tone = s.cloud.fail, pal.danger
		case s.cloud.ok != "":
			detail = s.cloud.ok
		}
		s.rowTone(c, pal, tr("Check the connection"), detail, tone, func() {
			if button(c, "cloud-verify", tr("Check"), sh.Configured(), pal) && s.cloud.busy == "" {
				s.verifyCloud()
			}
		})
	})
	s.group(c, pal, tr("Set up a worker"), tr("Paste the prompt into your coding agent (Claude Code, Codex, …). It deploys the worker with a D1 database and an R2 bucket on your account, asks which account and domain to use, and gives you the address and the token. Running it again updates the worker."), func() {
		s.row(c, pal, tr("Copy the setup prompt"), tr("The steps are in share-worker/DEPLOY.md of the Notes repository."), func() {
			if button(c, "cloud-copy-prompt", tr("Copy the prompt"), true, pal) {
				s.a.notebooks.clipboard.suppressFor(2 * time.Second)
				mygo.Clipboard.WriteText(share.SetupPrompt())
				s.toast = tr("The prompt is copied.")
			}
		})
	})
	if len(s.cloud.pages) > 0 {
		s.group(c, pal, tr("Published pages"), tr("Everything the worker holds, whichever vault it came from."), func() {
			for _, p := range s.cloud.pages {
				p := p
				s.row(c, pal, p.Title, p.URL, func() {
					if button(c, "cloud-copy:"+p.ID, tr("Copy link"), false, pal) {
						s.a.notebooks.clipboard.suppressFor(2 * time.Second)
						mygo.Clipboard.WriteText(p.URL)
						s.toast = tr("Copied.")
					}
					if button(c, "cloud-off:"+p.ID, tr("Take off the web"), false, pal) && s.cloud.busy == "" {
						s.unpublishCloud(p.ID)
					}
				})
			}
		})
	}
}
