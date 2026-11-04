package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"reflect"
	"strings"
	"testing"
)

// inbox is a fake Gmail API answering the profile with owner and recording each inserted
// message's query, labels and decoded raw message; refuse makes inserts fail for their scope.
type inbox struct {
	owner   string
	refuse  bool
	queries []string
	labels  [][]string
	raws    []string
}

func (b *inbox) serve(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/users/me/profile":
			json.NewEncoder(w).Encode(map[string]any{"emailAddress": b.owner})
		case r.Method == http.MethodPost && r.URL.Path == "/users/me/messages":
			if b.refuse {
				w.WriteHeader(http.StatusForbidden)
				io.WriteString(w, `{"error":{"code":403,"message":"Request had insufficient authentication scopes.","details":[{"reason":"ACCESS_TOKEN_SCOPE_INSUFFICIENT"}]}}`)
				return
			}
			var m struct {
				Raw      string   `json:"raw"`
				LabelIDs []string `json:"labelIds"`
			}
			if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
				t.Errorf("insert body: %v", err)
			}
			raw, err := base64.URLEncoding.DecodeString(m.Raw)
			if err != nil {
				t.Errorf("raw: %v", err)
			}
			b.queries = append(b.queries, r.URL.RawQuery)
			b.labels = append(b.labels, m.LabelIDs)
			b.raws = append(b.raws, string(raw))
			json.NewEncoder(w).Encode(map[string]any{"id": "n1"})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestInsertPutsAnUnreadMessageFromAndToTheOwner(t *testing.T) {
	b := &inbox{owner: "owner@example.com"}
	c := &Client{BaseURL: b.serve(t).URL, HTTP: http.DefaultClient}
	n := Notice{Subject: "categorize: 1 to review, ร้าน 1", Body: "run x: applied 1 of 1 changes\n  ร้านกาแฟ -3.00\n"}
	if err := c.Insert(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	if len(b.raws) != 1 {
		t.Fatalf("%d messages inserted; want 1", len(b.raws))
	}
	if b.queries[0] != "internalDateSource=receivedTime" {
		t.Errorf("query %q; want internalDateSource=receivedTime", b.queries[0])
	}
	if want := []string{"INBOX", "UNREAD"}; !reflect.DeepEqual(b.labels[0], want) {
		t.Errorf("labels %v; want %v", b.labels[0], want)
	}
	m, err := mail.ReadMessage(strings.NewReader(b.raws[0]))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(m.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Header.Get("From") != "owner@example.com" || m.Header.Get("To") != "owner@example.com" || subject != n.Subject {
		t.Errorf("From %q To %q Subject %q; want the owner both ways and %q", m.Header.Get("From"), m.Header.Get("To"), subject, n.Subject)
	}
	if _, err := m.Header.Date(); err != nil {
		t.Errorf("Date: %v", err)
	}
	if ct := m.Header.Get("Content-Type"); ct != "text/plain; charset=UTF-8" {
		t.Errorf("Content-Type %q", ct)
	}
	enc, _ := io.ReadAll(m.Body)
	body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(enc), "\r\n", ""))
	if err != nil || string(body) != n.Body {
		t.Errorf("body %q, %v; want %q", body, err, n.Body)
	}
}

func TestAnInsertRefusedForItsScopeSaysToRunAuth(t *testing.T) {
	b := &inbox{owner: "owner@example.com", refuse: true}
	c := &Client{BaseURL: b.serve(t).URL, HTTP: http.DefaultClient}
	if err := c.Insert(context.Background(), Notice{Subject: "s", Body: "b"}); !errors.Is(err, ErrAuth) {
		t.Errorf("Insert: %v; want ErrAuth", err)
	}
}

func TestANoticeWithHTMLIsMultipartAlternative(t *testing.T) {
	b := &inbox{owner: "owner@example.com"}
	c := &Client{BaseURL: b.serve(t).URL, HTTP: http.DefaultClient}
	n := Notice{Subject: "s", Body: "Run x · 1 of 1 changes applied\n  ร้านกาแฟ −3.00\n", HTML: "<!DOCTYPE html>\n<p>ร้านกาแฟ <b>−3.00</b></p>\n"}
	if err := c.Insert(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	m, err := mail.ReadMessage(strings.NewReader(b.raws[0]))
	if err != nil {
		t.Fatal(err)
	}
	media, params, err := mime.ParseMediaType(m.Header.Get("Content-Type"))
	if err != nil || media != "multipart/alternative" {
		t.Fatalf("Content-Type %q, %v; want multipart/alternative", m.Header.Get("Content-Type"), err)
	}
	r := multipart.NewReader(m.Body, params["boundary"])
	for _, want := range []struct{ contentType, body string }{{"text/plain; charset=UTF-8", n.Body}, {"text/html; charset=UTF-8", n.HTML}} {
		p, err := r.NextRawPart()
		if err != nil {
			t.Fatalf("part %s: %v", want.contentType, err)
		}
		enc, _ := io.ReadAll(p)
		body, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(string(enc), "\r\n", ""))
		if ct, te := p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"); ct != want.contentType || te != "base64" || err != nil || string(body) != want.body {
			t.Errorf("part %q %q: body %q, %v; want %q in base64", ct, te, body, err, want.body)
		}
	}
	if _, err := r.NextPart(); err != io.EOF {
		t.Errorf("after two parts: %v; want the end", err)
	}
}
