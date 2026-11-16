// Package share publishes pages to the user's own Notes Share worker: a
// Cloudflare Worker with a D1 database and an R2 bucket that the user deploys
// on their own account (share-worker/ in this repository). This package is the
// client of its HTTP API; it has no dependency on the window layer, and the
// address and the token it uses are the user's.
package share

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RequiredAPIVersion is the oldest worker API this version of Notes can talk to.
const RequiredAPIVersion = 1

// Errors a caller tells the user about.
var (
	ErrNotConfigured = errors.New("add your worker's address and token in Settings → Cloud")
	ErrUnauthorized  = errors.New("the worker did not accept the token: check it in Settings → Cloud")
	ErrNotShare      = errors.New("that address answered, but it is not a Notes Share worker: check the address")
)

// OutdatedError says the worker is older than this Notes needs.
type OutdatedError struct{ Version string }

func (e OutdatedError) Error() string {
	return fmt.Sprintf("your worker (version %s) is too old for this version of Notes: run the setup prompt again to update it", e.Version)
}

// MigrationsError says the worker's database is behind its code.
type MigrationsError struct{ Pending int }

func (e MigrationsError) Error() string {
	return fmt.Sprintf("your worker's database is %d update(s) behind: run the setup prompt again to finish updating it", e.Pending)
}

// ServerError is an answer the worker gave that is neither success nor one of the above.
type ServerError struct {
	Status  int
	Message string
}

func (e ServerError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("the worker answered with an error (%d)", e.Status)
}

// Info is what the worker says about itself.
type Info struct {
	App        string `json:"app"`
	APIVersion int    `json:"apiVersion"`
	Version    string `json:"version"`
	Migrations struct {
		Applied int `json:"applied"`
		Bundled int `json:"bundled"`
		Pending int `json:"pending"`
	} `json:"migrations"`
	Pages int `json:"pages"`
}

// Page is a page as it is sent to be published.
type Page struct {
	Title string `json:"title"`
	// HTML is the page's body: the worker adds the title, the breadcrumbs and the style.
	HTML     string `json:"html"`
	Icon     string `json:"icon,omitempty"`
	Parent   string `json:"parent,omitempty"`
	Position int    `json:"position,omitempty"`
	// Files are the names of every file the page uses, uploaded or not.
	Files []string `json:"files"`
}

// Reply is what the worker answers to a publish: where the page is, and which of its files it lacks.
type Reply struct {
	ID      string   `json:"id"`
	URL     string   `json:"url"`
	Missing []string `json:"missing"`
}

// Published is one page the worker holds.
type Published struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Parent    string    `json:"parent"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Client is one worker at one address with one token.
type Client struct {
	Base  *url.URL
	Token string
	// HTTP is the client to ask with; nil is a client that gives up after a minute.
	HTTP *http.Client
}

// NormalizeURL reads what the user typed as a worker's address:
// "notes-share.me.workers.dev" is "https://notes-share.me.workers.dev", and a
// local address is plain http.
func NormalizeURL(raw string) (*url.URL, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, ErrNotConfigured
	}
	if !strings.Contains(text, "://") {
		scheme := "https://"
		if strings.HasPrefix(text, "localhost") || strings.HasPrefix(text, "127.") {
			scheme = "http://"
		}
		text = scheme + text
	}
	u, err := url.Parse(strings.TrimRight(text, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%q is not a worker address", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment = "", ""
	return u, nil
}

// New is a client for the address and token the user entered.
func New(address, token string, transport http.RoundTripper) (*Client, error) {
	u, err := NormalizeURL(address)
	if err != nil {
		return nil, err
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrNotConfigured
	}
	return &Client{Base: u, Token: token, HTTP: &http.Client{Transport: transport, Timeout: 10 * time.Minute}}, nil
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader, length int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Base.String()+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Notes")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if body != nil {
		req.ContentLength = length
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: time.Minute}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, describe(err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, describe(err)
	}
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return data, nil
	case res.StatusCode == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	}
	var failure struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(data, &failure)
	if res.StatusCode == http.StatusNotFound && failure.Error == "" {
		return nil, ErrNotShare
	}
	return nil, ServerError{Status: res.StatusCode, Message: failure.Error}
}

func describe(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Timeout() {
		return errors.New("the worker took too long to answer")
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("could not reach the worker: %w", err)
}

// Info checks the address and the token together, and reads the deployed version.
func (c *Client) Info(ctx context.Context) (Info, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/v1/info", "", nil, 0)
	if err != nil {
		return Info{}, err
	}
	var info Info
	if json.Unmarshal(data, &info) != nil || info.App != "notes-share" {
		return Info{}, ErrNotShare
	}
	return info, nil
}

// Compatible is Info, and an error unless this worker can take what this Notes sends.
func (c *Client) Compatible(ctx context.Context) (Info, error) {
	info, err := c.Info(ctx)
	if err != nil {
		return info, err
	}
	if info.APIVersion < RequiredAPIVersion {
		return info, OutdatedError{Version: info.Version}
	}
	if info.Migrations.Pending > 0 {
		return info, MigrationsError{Pending: info.Migrations.Pending}
	}
	return info, nil
}

// Publish sends a page, or updates it in place at the same address.
func (c *Client) Publish(ctx context.Context, id string, page Page) (Reply, error) {
	if page.Files == nil {
		page.Files = []string{}
	}
	body, err := json.Marshal(page)
	if err != nil {
		return Reply{}, err
	}
	data, err := c.do(ctx, http.MethodPut, "/api/v1/pages/"+id, "application/json", bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return Reply{}, err
	}
	var reply Reply
	if err := json.Unmarshal(data, &reply); err != nil {
		return Reply{}, ErrNotShare
	}
	return reply, nil
}

// UploadFile sends one file of a published page.
func (c *Client) UploadFile(ctx context.Context, id, name, contentType string, data []byte) error {
	_, err := c.do(ctx, http.MethodPut, "/api/v1/pages/"+id+"/files/"+name, contentType, bytes.NewReader(data), int64(len(data)))
	return err
}

// Unpublish removes a page and its files. A page that is already gone is not an error.
func (c *Client) Unpublish(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/api/v1/pages/"+id, "", nil, 0)
	var server ServerError
	if errors.As(err, &server) && server.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// List is every page the worker holds, most recently updated first.
func (c *Client) List(ctx context.Context) ([]Published, error) {
	data, err := c.do(ctx, http.MethodGet, "/api/v1/pages", "", nil, 0)
	if err != nil {
		return nil, err
	}
	var listing struct {
		Pages []Published `json:"pages"`
	}
	if err := json.Unmarshal(data, &listing); err != nil {
		return nil, ErrNotShare
	}
	return listing.Pages, nil
}
