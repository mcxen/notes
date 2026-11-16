package frontend

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo/netproxy"

	"notes/internal/settings"
	"notes/internal/share"
)

// ShareStatus is what a page needs to know to offer publishing: never the token.
type ShareStatus struct {
	// Enabled is the switch in Settings › Cloud.
	Enabled bool `json:"enabled"`
	// Configured says a worker address and a token are entered.
	Configured bool `json:"configured"`
	// URL is the worker's address.
	URL string `json:"url"`
	// MaxFileMB is the largest file a published page carries.
	MaxFileMB int `json:"maxFileMB"`
	// Publications are the pages of this vault that are published, by page id.
	Publications map[string]share.Publication `json:"publications"`
}

// ShareReply is what the worker answered to a published page.
type ShareReply = share.Reply

var (
	publicationsOnce sync.Once
	publications     *share.Store
)

// shareStore is the publications file, next to the settings: on this computer only.
func shareStore() *share.Store {
	publicationsOnce.Do(func() {
		publications = share.OpenStore(filepath.Join(filepath.Dir(settingsPath()), "publications.json"))
	})
	return publications
}

var errShareOff = errors.New("publishing to the web is off: turn it on in Settings › Cloud")

// shareClient is the client of the worker the user entered, when publishing is on.
func shareClient() (*share.Client, settings.Share, error) {
	st, err := settings.Load(settingsPath())
	if err != nil {
		return nil, settings.Share{}, err
	}
	if !st.Share.Enabled {
		return nil, st.Share, errShareOff
	}
	client, err := share.New(st.Share.URL, st.Share.Token, netproxy.Transport())
	return client, st.Share, err
}

// ShareStatus says whether this window may publish, and what of this vault is published.
func (n *Notebooks) ShareStatus(ctx context.Context) (ShareStatus, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return ShareStatus{}, err
	}
	st, err := settings.Load(settingsPath())
	if err != nil {
		return ShareStatus{}, err
	}
	out := ShareStatus{Enabled: st.Share.Enabled, Configured: st.Share.Configured(), URL: st.Share.URL, MaxFileMB: st.Share.FileMB(), Publications: map[string]share.Publication{}}
	if out.Enabled {
		out.Publications, err = shareStore().Of(s.vault.Info().ID)
	}
	return out, err
}

// ShareCheck asks the worker whether it can take what this Notes sends: the
// address and the token are right, and its version and database are not behind.
func (n *Notebooks) ShareCheck(ctx context.Context) error {
	client, _, err := shareClient()
	if err != nil {
		return err
	}
	_, err = client.Compatible(ctx)
	return err
}

// SharePublish sends a page to the worker, or updates it in place, and keeps
// where it is. `id` is the page's address on the worker, chosen by the page the
// first time: the same id updates it. The reply says which of its files the
// worker lacks (ShareUpload).
func (n *Notebooks) SharePublish(ctx context.Context, pageID, id string, page share.Page) (ShareReply, error) {
	s, err := n.callerSession(ctx)
	if err != nil {
		return ShareReply{}, err
	}
	client, _, err := shareClient()
	if err != nil {
		return ShareReply{}, err
	}
	reply, err := client.Publish(ctx, id, page)
	if err != nil {
		return ShareReply{}, err
	}
	err = shareStore().Set(s.vault.Info().ID, pageID, share.Publication{ID: reply.ID, URL: reply.URL, Parent: page.Parent, Published: time.Now().UTC()})
	return reply, err
}

// ShareUpload sends one file of a published page.
func (n *Notebooks) ShareUpload(ctx context.Context, id, name, contentType string, data []byte) error {
	client, cfg, err := shareClient()
	if err != nil {
		return err
	}
	if limit := cfg.FileMB() << 20; len(data) > limit {
		return errors.New("that file is larger than the limit in Settings › Cloud")
	}
	return client.UploadFile(ctx, id, name, contentType, data)
}

// ShareUnpublish takes a page off the web: the worker deletes it and its files,
// and Notes forgets where it was.
func (n *Notebooks) ShareUnpublish(ctx context.Context, pageID string) error {
	s, err := n.callerSession(ctx)
	if err != nil {
		return err
	}
	client, _, err := shareClient()
	if err != nil {
		return err
	}
	vaultID := s.vault.Info().ID
	known, err := shareStore().Of(vaultID)
	if err != nil {
		return err
	}
	pub, ok := known[pageID]
	if !ok {
		return nil
	}
	if err := client.Unpublish(ctx, pub.ID); err != nil {
		return err
	}
	return shareStore().Forget(vaultID, pageID)
}
