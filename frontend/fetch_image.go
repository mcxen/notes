package frontend

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/egoist/mygo/netproxy"

	"notes/internal/fetchimg"
	"notes/internal/settings"
)

// FetchedImage is a picture downloaded for a page that was pasted from the web.
type FetchedImage struct {
	// Data is the picture's bytes, base64-encoded.
	Data string `json:"data"`
	// Type is its media type, found from the bytes.
	Type string `json:"type"`
	// Name is a file name for it.
	Name string `json:"name"`
}

// pictureFetcher asks public hosts only, through the proxy the system uses.
var pictureFetcher = &fetchimg.Fetcher{Transport: netproxy.Transport(), Proxy: netproxy.Proxy}

// FetchImage downloads a picture a pasted web page named, so that it is kept
// with the note. It refuses addresses that are not on the open internet and
// anything that is not a picture; the page keeps a link in that case. The
// download can be turned off in Settings › General.
func (n *Notebooks) FetchImage(ctx context.Context, address string) (FetchedImage, error) {
	st, err := settings.Load(settingsPath())
	if err == nil && st.NoPasteImages {
		return FetchedImage{}, errors.New("downloading pictures on paste is turned off")
	}
	img, err := pictureFetcher.Fetch(ctx, address)
	if err != nil {
		return FetchedImage{}, err
	}
	return FetchedImage{Data: base64.StdEncoding.EncodeToString(img.Data), Type: img.Type, Name: img.Name}, nil
}
