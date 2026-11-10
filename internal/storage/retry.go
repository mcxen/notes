package storage

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrBusy is what a server answers with 429 or 503 when it asks the client to
// slow down (Jianguoyun rate-limits this way). Operations that fail with it
// are retried by WithRetry.
var ErrBusy = errors.New("storage: the server asked us to slow down")

// busy turns a rate-limit answer into ErrBusy; any other error is unchanged.
func busy(err error) error {
	if isStatus(err, http.StatusTooManyRequests) || isStatus(err, http.StatusServiceUnavailable) {
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
	return err
}

// maxAttempts is how many times an operation is tried: the first, then
// retries after 1, 2, 4 and 8 seconds.
const maxAttempts = 5

// WithRetry wraps a remote so that an operation the server refused for being
// busy waits and tries again, with exponential backoff.
func WithRetry(r Remote) Remote {
	return &retrying{Remote: r, sleep: sleepCtx}
}

type retrying struct {
	Remote
	sleep func(ctx context.Context, d time.Duration) error
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *retrying) again(ctx context.Context, op func() error) error {
	wait := time.Second
	for attempt := 1; ; attempt++ {
		err := op()
		if !errors.Is(err, ErrBusy) || attempt == maxAttempts {
			return err
		}
		if serr := r.sleep(ctx, wait); serr != nil {
			return serr
		}
		wait *= 2
	}
}

func (r *retrying) List(ctx context.Context) ([]Entry, error) {
	var out []Entry
	err := r.again(ctx, func() error {
		var err error
		out, err = r.Remote.List(ctx)
		return err
	})
	return out, err
}

func (r *retrying) Get(ctx context.Context, p string) ([]byte, error) {
	var out []byte
	err := r.again(ctx, func() error {
		var err error
		out, err = r.Remote.Get(ctx, p)
		return err
	})
	return out, err
}

func (r *retrying) Put(ctx context.Context, p string, data []byte) (Entry, error) {
	var out Entry
	err := r.again(ctx, func() error {
		var err error
		out, err = r.Remote.Put(ctx, p, data)
		return err
	})
	return out, err
}

func (r *retrying) Move(ctx context.Context, from, to string) error {
	return r.again(ctx, func() error { return r.Remote.Move(ctx, from, to) })
}
