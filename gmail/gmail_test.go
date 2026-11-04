package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

var (
	from = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	to   = time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)
)

// mailbox is a fake Gmail API listing pages of message ids joined by nextPageToken and
// serving the messages by id. It records each list request's q; it fails a list request past
// the last page and a get of an unknown id, so a read that goes wrong fails an assertion.
func mailbox(t *testing.T, pages [][]string, msgs map[string]map[string]any) (srv *httptest.Server, queries *[]string) {
	t.Helper()
	queries = &[]string{}
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/users/me/messages" {
			q := r.URL.Query()
			*queries = append(*queries, q.Get("q"))
			if q.Get("maxResults") != "500" {
				t.Errorf("maxResults = %q; want 500", q.Get("maxResults"))
			}
			page := 0
			if tok := q.Get("pageToken"); tok != "" {
				page, _ = strconv.Atoi(tok)
			}
			if page >= len(pages) {
				t.Errorf("page %d: the list should have ended", page)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			var refs []map[string]string
			for _, id := range pages[page] {
				refs = append(refs, map[string]string{"id": id, "threadId": id})
			}
			body := map[string]any{"messages": refs}
			if page+1 < len(pages) {
				body["nextPageToken"] = strconv.Itoa(page + 1)
			}
			json.NewEncoder(w).Encode(body)
			return
		}
		id, ok := strings.CutPrefix(r.URL.Path, "/users/me/messages/")
		m, known := msgs[id]
		if !ok || !known {
			t.Errorf("unexpected request %s", r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if f := r.URL.Query().Get("format"); f != "full" {
			t.Errorf("format = %q; want full", f)
		}
		json.NewEncoder(w).Encode(m)
	}))
	t.Cleanup(srv.Close)
	return srv, queries
}

// fakeMessage is a message as the API returns it with format=full.
func fakeMessage(id string, received time.Time, to string, payload map[string]any) map[string]any {
	payload["headers"] = []map[string]string{{"name": "Subject", "value": "Your Grab E-Receipt"}, {"name": "To", "value": to}}
	return map[string]any{"id": id, "internalDate": strconv.FormatInt(received.UnixMilli(), 10), "payload": payload}
}

// fakePart is a MIME part with body data in enc, or with parts nested in it.
func fakePart(mime string, enc *base64.Encoding, data string, parts ...map[string]any) map[string]any {
	p := map[string]any{"mimeType": mime, "body": map[string]any{"size": 0}}
	if data != "" {
		p["body"] = map[string]any{"size": len(data), "data": enc.EncodeToString([]byte(data))}
	}
	if len(parts) > 0 {
		p["parts"] = parts
	}
	return p
}

func TestMessagesPlainText(t *testing.T) {
	received := time.Date(2026, 3, 12, 8, 15, 30, 0, time.FixedZone("ICT", 7*3600))
	srv, _ := mailbox(t, [][]string{{"m1"}}, map[string]map[string]any{
		"m1": fakeMessage("m1", received, "Test <a@example.com>", fakePart("multipart/alternative", nil, "",
			fakePart("text/plain", base64.RawURLEncoding, "Your Grab receipt\nTotal ฿78"),
			fakePart("text/html", base64.URLEncoding, "<p>Your Grab receipt</p>"))),
	})
	got, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []Message{{ID: "m1", Received: received.UTC(), To: "a@example.com", Text: "Your Grab receipt\nTotal ฿78"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v\nwant %#v", got, want)
	}
	if len(got) == 1 && got[0].Received.Location() != time.UTC {
		t.Errorf("Received in %v; want UTC", got[0].Received.Location())
	}
}

func TestMessagesHTMLOnly(t *testing.T) {
	html := `<html><style>x{}</style><body><p>Total Paid</p><td>฿78</td></body></html>`
	srv, _ := mailbox(t, [][]string{{"m1"}}, map[string]map[string]any{
		"m1": fakeMessage("m1", from, "a@example.com", fakePart("multipart/mixed", nil, "",
			fakePart("multipart/related", nil, "", fakePart("text/html", base64.URLEncoding, html)))),
	})
	got, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "Total Paid ฿78" {
		t.Errorf("got %#v; want one message with Text %q", got, "Total Paid ฿78")
	}
}

func TestMessagesBlankPlainFallsBackToHTML(t *testing.T) {
	for _, plain := range []string{"", " \r\n\t\n"} {
		srv, _ := mailbox(t, [][]string{{"m1"}}, map[string]map[string]any{
			"m1": fakeMessage("m1", from, "a@example.com", fakePart("multipart/alternative", nil, "",
				fakePart("text/plain", base64.URLEncoding, plain),
				fakePart("text/html", base64.URLEncoding, "<p>Total Paid</p><td>฿78</td>"))),
		})
		got, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Text != "Total Paid ฿78" {
			t.Errorf("plain %q: got %#v; want one message with the HTML's text %q", plain, got, "Total Paid ฿78")
		}
	}
}

func TestMessagesPaging(t *testing.T) {
	msgs := map[string]map[string]any{}
	for i, id := range []string{"m1", "m2", "m3"} {
		msgs[id] = fakeMessage(id, from.Add(time.Duration(i)*time.Hour), "a@example.com", fakePart("text/plain", base64.URLEncoding, "receipt "+id))
	}
	srv, _ := mailbox(t, [][]string{{"m1", "m2"}, {"m3"}}, msgs)
	got, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, m := range got {
		ids = append(ids, m.ID+":"+m.Text)
	}
	if want := []string{"m1:receipt m1", "m2:receipt m2", "m3:receipt m3"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("got %v; want %v", ids, want)
	}
}

func TestMessagesQuery(t *testing.T) {
	srv, queries := mailbox(t, [][]string{nil}, nil)
	if _, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to); err != nil {
		t.Fatal(err)
	}
	want := "from:x after:" + strconv.FormatInt(from.Unix(), 10) + " before:" + strconv.FormatInt(to.AddDate(0, 0, 1).Unix(), 10)
	if !reflect.DeepEqual(*queries, []string{want}) {
		t.Errorf("q = %q; want [%q]", *queries, want)
	}
}

func TestMessagesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"message":"backend"}}`))
	}))
	defer srv.Close()
	_, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
	if err == nil || !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "backend") ||
		strings.Contains(err.Error(), srv.URL) || strings.Contains(err.Error(), "/users/me") {
		t.Errorf("err = %v; want the status and the API's message without the request", err)
	}
}

func TestMessagesAuth(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		auth   bool
	}{
		{"unauthorized", http.StatusUnauthorized, `{"error":{"code":401,"message":"Request had invalid authentication credentials."}}`, true},
		{"scope", http.StatusForbidden, `{"error":{"code":403,"message":"Request had insufficient authentication scopes.","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`, true},
		{"permissions", http.StatusForbidden, `{"error":{"code":403,"message":"Insufficient Permission","errors":[{"reason":"insufficientPermissions"}]}}`, true},
		{"disabled", http.StatusForbidden, `{"error":{"message":"Gmail API has not been used in project 1 before or it is disabled."}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := (&Client{BaseURL: srv.URL, HTTP: srv.Client()}).Messages(context.Background(), "from:x", from, to)
			if tc.auth {
				if !errors.Is(err, ErrAuth) {
					t.Errorf("err = %v; want ErrAuth", err)
				}
				return
			}
			if err == nil || errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "has not been used in project 1") {
				t.Errorf("err = %v; want the API's message, not ErrAuth", err)
			}
		})
	}
}
