package retry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// waits records the pauses a transport takes without taking them.
type waits []time.Duration

func (w *waits) sleep(ctx context.Context, d time.Duration) error {
	*w = append(*w, d)
	return ctx.Err()
}

func transport(w *waits) *Transport {
	return &Transport{Base: http.DefaultTransport, Timeout: 100 * time.Millisecond, Waits: Waits, Sleep: w.sleep}
}

// failing answers status to the first n requests and 200 "ok" after.
func failing(t *testing.T, n int32, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= n {
			w.WriteHeader(status)
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func get(t *testing.T, tr *Transport, url string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return (&http.Client{Transport: tr}).Do(req)
}

func TestA503IsRetriedUntilAnAttemptSucceeds(t *testing.T) {
	srv, calls := failing(t, 2, http.StatusServiceUnavailable)
	var w waits
	resp, err := get(t, transport(&w), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "ok" || calls.Load() != 3 {
		t.Errorf("got %d %q after %d calls; want 200 ok after 3", resp.StatusCode, body, calls.Load())
	}
	if want := (waits{10 * time.Second, 30 * time.Second}); !reflect.DeepEqual(w, want) {
		t.Errorf("waits %v; want %v", w, want)
	}
}

func TestATimeoutIsRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
			return
		}
		io.WriteString(w, "ok")
	}))
	t.Cleanup(srv.Close)
	var w waits
	resp, err := get(t, transport(&w), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Errorf("got %d after %d calls; want 200 after 2", resp.StatusCode, calls.Load())
	}
}

func TestARequestStillFailingAfterItsFourthAttemptReturnsItsLastResponse(t *testing.T) {
	srv, calls := failing(t, 100, http.StatusTooManyRequests)
	var w waits
	resp, err := get(t, transport(&w), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || calls.Load() != 4 {
		t.Errorf("got %d after %d calls; want 429 after 4", resp.StatusCode, calls.Load())
	}
	if want := (waits{10 * time.Second, 30 * time.Second, 90 * time.Second}); !reflect.DeepEqual(w, want) {
		t.Errorf("waits %v; want %v", w, want)
	}
}

func TestA404IsNotRetried(t *testing.T) {
	srv, calls := failing(t, 100, http.StatusNotFound)
	var w waits
	resp, err := get(t, transport(&w), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || calls.Load() != 1 || len(w) != 0 {
		t.Errorf("got %d after %d calls and waits %v; want 404 after 1 and no waits", resp.StatusCode, calls.Load(), w)
	}
}

func TestARetriedPUTSendsItsBodyAgain(t *testing.T) {
	var calls atomic.Int32
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPut, srv.URL, strings.NewReader(`{"category_id":7}`))
	if err != nil {
		t.Fatal(err)
	}
	var w waits
	resp, err := (&http.Client{Transport: transport(&w)}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if want := []string{`{"category_id":7}`, `{"category_id":7}`}; !reflect.DeepEqual(bodies, want) {
		t.Errorf("bodies %q; want %q", bodies, want)
	}
}

func TestACancelledContextEndsTheWait(t *testing.T) {
	srv, calls := failing(t, 100, http.StatusServiceUnavailable)
	ctx, cancel := context.WithCancel(context.Background())
	tr := &Transport{Base: http.DefaultTransport, Timeout: time.Second, Waits: Waits, Sleep: func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&http.Client{Transport: tr}).Do(req)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Errorf("err %v after %d calls; want context.Canceled after 1", err, calls.Load())
	}
}

func TestTheDefaultSleepEndsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := (&Transport{}).sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) || time.Since(start) > time.Second {
		t.Errorf("sleep: %v after %v; want context.Canceled at once", err, time.Since(start))
	}
}

func TestABodyIsReadableAfterTheResponseReturns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		time.Sleep(20 * time.Millisecond)
		io.WriteString(w, strings.Repeat("x", 1<<16))
	}))
	t.Cleanup(srv.Close)
	var w waits
	resp, err := get(t, transport(&w), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || len(body) != 1<<16 {
		t.Errorf("read %d bytes, %v; want %d bytes", len(body), err, 1<<16)
	}
}

func TestClientRetriesWithTheDefaultWaitsAndNoOverallTimeout(t *testing.T) {
	c := Client(30 * time.Second)
	tr, ok := c.Transport.(*Transport)
	if !ok || c.Timeout != 0 || tr.Timeout != 30*time.Second || !reflect.DeepEqual(tr.Waits, Waits) {
		t.Errorf("Client(30s) = %+v with transport %+v; want a retrying transport limiting each attempt to 30s", c, c.Transport)
	}
}
