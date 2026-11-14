// Package fetchimg downloads a picture a web page named, for the paste of
// content copied from a web page: the page's HTML holds only the picture's
// address, and its bytes are kept with the note. The address is untrusted (any
// page on the web may write it), so only public hosts are asked, only pictures
// of the kinds a page draws are taken, and the size and the time are held.
package fetchimg

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"
)

const (
	// MaxBytes is the largest picture taken: what a page's file takes.
	MaxBytes = 7<<20 + 1<<19 // 7.5 MB
	// Timeout is how long a picture may take, redirects included.
	Timeout     = 25 * time.Second
	maxRedirect = 5
)

// Image is a picture fetched.
type Image struct {
	Data []byte
	// Type is the media type found from the bytes, not what the server said.
	Type string
	// Name is a file name for it, from the address.
	Name string
}

// ErrNotPicture is an answer that is not a picture a page can draw.
var ErrNotPicture = errors.New("the address is not a picture of a kind a page can show")

// Fetcher downloads pictures. The zero value is ready: it reaches public
// hosts directly.
type Fetcher struct {
	// Transport carries the requests; nil is http's default.
	Transport http.RoundTripper
	// Resolve looks a host up; nil is the system's resolver.
	Resolve func(ctx context.Context, host string) ([]net.IP, error)
	// Allow says whether an address may be asked; nil allows public ones only.
	Allow func(net.IP) bool
	// Proxy says which proxy a request goes through, nil for none (netproxy.Proxy).
	// Behind a proxy the proxy looks the host up, so the host's name is not
	// checked here: a name that a VPN or a proxy maps to a private range is
	// no sign of an unsafe host. An address written as numbers is always checked.
	Proxy func(*http.Request) (*url.URL, error)
}

func (f *Fetcher) allow(ip net.IP) bool {
	if f.Allow != nil {
		return f.Allow(ip)
	}
	return public(ip)
}

// public says whether an address is one on the open internet: not this
// computer, not a home or office network, not a link-local or reserved one.
func public(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 100 && v4[1]&0xC0 == 64: // 100.64.0.0/10, carrier-grade NAT
			return false
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0: // 192.0.0.0/24
			return false
		case v4[0] >= 240: // reserved, broadcast
			return false
		}
	}
	return true
}

// check refuses an address that is not for the open internet: its scheme,
// its host's addresses.
func (f *Fetcher) check(ctx context.Context, u *url.URL, proxied bool) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("only http and https addresses are fetched, not %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("the address has no host")
	}
	var ips []net.IP
	if ip := net.ParseIP(host); ip != nil {
		ips = []net.IP{ip}
	} else if proxied {
		return nil
	} else {
		resolve := f.Resolve
		if resolve == nil {
			resolve = func(ctx context.Context, host string) ([]net.IP, error) {
				addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				out := make([]net.IP, 0, len(addrs))
				for _, a := range addrs {
					out = append(out, a.IP)
				}
				return out, err
			}
		}
		var err error
		if ips, err = resolve(ctx, host); err != nil {
			return err
		}
	}
	if len(ips) == 0 {
		return fmt.Errorf("no address for %s", host)
	}
	for _, ip := range ips {
		if !f.allow(ip) {
			return fmt.Errorf("%s is not a host on the open internet", host)
		}
	}
	return nil
}

// guard asks the Fetcher about every hop of a request, redirects included,
// before it is made.
type guard struct {
	f    *Fetcher
	next http.RoundTripper
}

func (g guard) RoundTrip(r *http.Request) (*http.Response, error) {
	proxied := false
	if g.f.Proxy != nil {
		if p, err := g.f.Proxy(r); err == nil && p != nil {
			proxied = true
		}
	}
	if err := g.f.check(r.Context(), r.URL, proxied); err != nil {
		return nil, err
	}
	return g.next.RoundTrip(r)
}

// Fetch downloads the picture at address.
func (f *Fetcher) Fetch(ctx context.Context, address string) (Image, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return Image{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	next := f.Transport
	if next == nil {
		next = http.DefaultTransport
	}
	client := &http.Client{
		Transport: guard{f: f, next: next},
		CheckRedirect: func(_ *http.Request, via []*http.Request) error {
			if len(via) > maxRedirect {
				return errors.New("too many redirects")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Image{}, err
	}
	// What a browser sends: some hosts answer a program with nothing, and some
	// serve a picture only to a page of their own, which a Referer of the host
	// itself satisfies.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Notes) AppleWebKit/537.36 (KHTML, like Gecko)")
	req.Header.Set("Accept", "image/png,image/jpeg,image/gif,image/webp,image/*;q=0.8")
	req.Header.Set("Referer", u.Scheme+"://"+u.Host+"/")
	resp, err := client.Do(req)
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Image{}, fmt.Errorf("the host answered %s", resp.Status)
	}
	if resp.ContentLength > MaxBytes {
		return Image{}, fmt.Errorf("the picture is over %d MB", MaxBytes>>20)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return Image{}, err
	}
	if len(data) > MaxBytes {
		return Image{}, fmt.Errorf("the picture is over %d MB", MaxBytes>>20)
	}
	kind := kindOf(data)
	if kind == "" {
		return Image{}, ErrNotPicture
	}
	return Image{Data: data, Type: kind, Name: nameOf(resp.Request.URL, kind)}, nil
}

// kindOf is the picture's type from its first bytes, for the kinds a page
// draws and a note keeps. A page's SVG is not taken: it may hold scripts and
// styles, and is kept as a link.
func kindOf(data []byte) string {
	head := data
	if len(head) > 512 {
		head = head[:512]
	}
	switch kind := strings.SplitN(http.DetectContentType(head), ";", 2)[0]; kind {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
		return kind
	}
	return ""
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

var extensions = map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp", "image/bmp": ".bmp"}

// nameOf is a file name for a picture: the last part of its address, with the
// extension its type has.
func nameOf(u *url.URL, kind string) string {
	name := unsafeName.ReplaceAllString(path.Base(u.Path), "-")
	name = strings.Trim(name, "-.")
	if len(name) > 60 {
		name = name[len(name)-60:]
	}
	want := extensions[kind]
	if name == "" || name == "/" {
		return "image" + want
	}
	if ext := strings.ToLower(path.Ext(name)); ext != want && !(want == ".jpg" && ext == ".jpeg") {
		name = strings.TrimSuffix(name, path.Ext(name)) + want
	}
	return name
}
