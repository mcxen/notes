package fetchimg

import (
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

var png1x1, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")

// local lets a test reach the server it started on this computer.
func local() *Fetcher { return &Fetcher{Allow: func(net.IP) bool { return true }} }

func TestAPictureIsFetchedWithItsTypeAndName(t *testing.T) {
	var referer, agent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		referer, agent = r.Header.Get("Referer"), r.Header.Get("User-Agent")
		// A server may say anything: the type is found from the bytes.
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(png1x1)
	}))
	defer srv.Close()
	img, err := local().Fetch(context.Background(), srv.URL+"/a/b/logo%20big.jpeg?size=2")
	if err != nil {
		t.Fatal(err)
	}
	if img.Type != "image/png" || !bytes.Equal(img.Data, png1x1) {
		t.Fatalf("image = %s, %d bytes", img.Type, len(img.Data))
	}
	if img.Name != "logo-big.png" {
		t.Errorf("name = %q: the extension follows the type", img.Name)
	}
	if !strings.HasPrefix(referer, "http://127.0.0.1") || !strings.Contains(agent, "Mozilla") {
		t.Errorf("referer %q, agent %q", referer, agent)
	}
}

func TestWhatIsNotAPictureIsRefused(t *testing.T) {
	for name, body := range map[string]string{
		"html":   "<html><body>hello</body></html>",
		"svg":    `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"script": "alert(1)",
		"empty":  "",
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png") // a lie
			w.Write([]byte(body))
		}))
		_, err := local().Fetch(context.Background(), srv.URL+"/x.png")
		srv.Close()
		if err != ErrNotPicture {
			t.Errorf("%s: %v, want ErrNotPicture", name, err)
		}
	}
}

func TestAnAnswerThatIsNotOKOrIsTooLargeIsRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/gone":
			http.NotFound(w, r)
		case "/big":
			w.Write(png1x1)
			w.Write(make([]byte, MaxBytes)) // over the limit, with no length said
		}
	}))
	defer srv.Close()
	if _, err := local().Fetch(context.Background(), srv.URL+"/gone"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a missing picture: %v", err)
	}
	if _, err := local().Fetch(context.Background(), srv.URL+"/big"); err == nil || !strings.Contains(err.Error(), "MB") {
		t.Errorf("a picture over the limit: %v", err)
	}
}

func TestRedirectsAreFollowedAndEachHopIsChecked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(png1x1) }))
	defer target.Close()
	hop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final.png", http.StatusFound)
	}))
	defer hop.Close()
	if img, err := local().Fetch(context.Background(), hop.URL+"/start"); err != nil || img.Name != "final.png" {
		t.Fatalf("a redirect: %v %+v", err, img)
	}

	// A public host that sends the request to a private one is stopped at the hop.
	f := &Fetcher{
		Resolve: func(_ context.Context, host string) ([]net.IP, error) {
			if host == "public.example" {
				return []net.IP{net.ParseIP("93.184.216.34")}, nil
			}
			return []net.IP{net.ParseIP("10.0.0.5")}, nil
		},
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "public.example" {
				return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"http://internal.example/secret.png"}}, Body: http.NoBody, Request: r}, nil
			}
			t.Error("a private host was asked")
			return nil, nil
		}),
	}
	if _, err := f.Fetch(context.Background(), "http://public.example/a.png"); err == nil || !strings.Contains(err.Error(), "open internet") {
		t.Errorf("a redirect into a private host: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOnlyPublicHostsAreAsked(t *testing.T) {
	asked := false
	f := &Fetcher{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { asked = true; return nil, nil })}
	for _, address := range []string{
		"http://127.0.0.1/a.png", "http://localhost/a.png", "http://[::1]/a.png", "http://10.1.2.3/a.png", "http://192.168.0.1/a.png",
		"http://172.16.5.5/a.png", "http://169.254.169.254/latest/meta-data", "http://0.0.0.0/a.png", "http://100.64.0.1/a.png",
		"http://[fe80::1]/a.png", "http://[fd00::1]/a.png", "http://[::ffff:127.0.0.1]/a.png",
		"file:///etc/passwd", "ftp://example.com/a.png", "data:image/png;base64,AAAA", "javascript:alert(1)", "http:///a.png",
	} {
		if _, err := f.Fetch(context.Background(), address); err == nil {
			t.Errorf("%s was fetched", address)
		}
	}
	if asked {
		t.Fatal("a request was made to a host that is not public")
	}
}

// Behind a proxy the proxy looks a name up: a name that a VPN or a proxy
// maps to a private range (a fake address) is not refused, a number still is.
func TestBehindAProxyANameIsNotCheckedButANumberIs(t *testing.T) {
	proxy, _ := url.Parse("http://127.0.0.1:7890")
	asked := 0
	f := &Fetcher{
		Resolve:   func(context.Context, string) ([]net.IP, error) { return []net.IP{net.ParseIP("198.18.0.7")}, nil },
		Proxy:     func(*http.Request) (*url.URL, error) { return proxy, nil },
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) { asked++; return nil, http.ErrHandlerTimeout }),
	}
	f.Fetch(context.Background(), "https://raw.githubusercontent.com/a/b.png")
	if asked != 1 {
		t.Errorf("a named host behind a proxy was not asked")
	}
	if _, err := f.Fetch(context.Background(), "http://10.0.0.1/a.png"); err == nil || asked != 1 {
		t.Errorf("a private number behind a proxy was asked")
	}
	// The system's own fake range is an address, not an attack.
	if !public(net.ParseIP("198.18.0.7")) {
		t.Error("the range VPNs use for names is refused")
	}
}
