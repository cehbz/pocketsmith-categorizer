// Package retry retries HTTP requests that failed transiently.
package retry

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Waits are the pauses between a request's attempts: four attempts in all.
var Waits = []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second}

// Transport sends each request through Base, each attempt limited to Timeout, and retries one
// that failed in transport or got 429 or a 5xx, pausing Waits between attempts. A request whose
// body can't be replayed gets one attempt. The last attempt's response or error is returned.
type Transport struct {
	Base    http.RoundTripper
	Timeout time.Duration
	Waits   []time.Duration
	Sleep   func(ctx context.Context, d time.Duration) error // nil waits on a timer
}

// Client is an HTTP client whose requests retry with Waits, each attempt limited to timeout.
// It has no overall timeout, which would bound all attempts together.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Transport: &Transport{Base: http.DefaultTransport, Timeout: timeout, Waits: Waits}}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		resp, err := t.once(req, attempt)
		if attempt == len(t.Waits) || !replayable(req) || req.Context().Err() != nil || !transient(resp, err) {
			return resp, err
		}
		if resp != nil {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if err := t.sleep(req.Context(), t.Waits[attempt]); err != nil {
			return nil, err
		}
	}
}

// once is one attempt at req, its body replayed after the first, limited to Timeout until its
// response body is closed.
func (t *Transport) once(req *http.Request, attempt int) (*http.Response, error) {
	r := req
	if attempt > 0 && req.Body != nil && req.Body != http.NoBody {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		r = req.Clone(req.Context())
		r.Body = body
	}
	if t.Timeout <= 0 {
		return t.Base.RoundTrip(r)
	}
	ctx, cancel := context.WithTimeout(req.Context(), t.Timeout)
	resp, err := t.Base.RoundTrip(r.WithContext(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelling{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func (t *Transport) sleep(ctx context.Context, d time.Duration) error {
	if t.Sleep != nil {
		return t.Sleep(ctx, d)
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// replayable reports whether req's body can be sent again.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// transient reports whether an attempt failed in a way a later one may not: in transport, or
// with 429 or a 5xx.
func transient(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
}

// cancelling is a response body that ends its attempt's deadline when closed.
type cancelling struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *cancelling) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	return err
}
