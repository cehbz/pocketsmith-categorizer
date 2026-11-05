// Package gmail reads the user's Gmail messages for the receipts layer and inserts notices
// into their inbox.
package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/mail"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// BaseURL is the Gmail API v1.
const BaseURL = "https://gmail.googleapis.com/gmail/v1"

// Scope reads mail and nothing else.
const Scope = "https://www.googleapis.com/auth/gmail.readonly"

// InsertScope adds messages to the mailbox and nothing else.
const InsertScope = "https://www.googleapis.com/auth/gmail.insert"

// Notice is an email for the mailbox's owner.
type Notice struct {
	Subject string
	Body    string // plain text
	HTML    string // the same as an HTML page; empty for a plain-text notice
}

// ErrAuth is a token that is missing, expired without a way to refresh it, revoked, or
// without the scope the call needs (Scope to read, InsertScope to insert).
var ErrAuth = errors.New("gmail: no usable token: run categorize auth")

const pageSize = 500

// Gmail's per-user quota is per minute: a read it refuses waits out the minute and tries
// again, this many times.
const (
	rateWait    = 65 * time.Second
	rateRetries = 5
)

// errRateLimited is a 403 refusing a request for Gmail's per-user rate or quota.
var errRateLimited = errors.New("rate limited")

// Client reads the user's messages through an HTTP client that authorizes its requests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	Log     io.Writer                                        // where a message the listing repeats is noted; nil discards
	Sleep   func(ctx context.Context, d time.Duration) error // waits out a rate limit; nil waits on a timer
}

// Message is one message's arrival, sender, recipient, subject and text.
type Message struct {
	ID       string
	Received time.Time
	From     string // the bare address
	To       string // the bare address
	Subject  string
	Text     string
}

type message struct {
	ID           string `json:"id"`
	InternalDate int64  `json:"internalDate,string"` // milliseconds since the epoch
	Payload      part   `json:"payload"`
}

type part struct {
	MimeType string `json:"mimeType"`
	Headers  []struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"headers"`
	Body struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []part `json:"parts"`
}

// Messages reads the messages query finds that arrived from from to the end of to's day, a
// page of ids at a time, each message once and in full, but for those skip says to leave;
// a nil skip leaves none.
func (c *Client) Messages(ctx context.Context, query string, from, to time.Time, skip func(id string) bool) ([]Message, error) {
	q := url.Values{
		"q":          {fmt.Sprintf("%s after:%d before:%d", query, from.Unix(), to.AddDate(0, 0, 1).Unix())},
		"maxResults": {strconv.Itoa(pageSize)},
	}
	var out []Message
	seen := map[string]bool{}
	for {
		var page struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			Next string `json:"nextPageToken"`
		}
		if err := c.get(ctx, "/users/me/messages", q, &page); err != nil {
			return nil, err
		}
		for _, ref := range page.Messages {
			if seen[ref.ID] {
				if c.Log != nil {
					fmt.Fprintf(c.Log, "gmail: %q listed message %s again; read once\n", query, ref.ID)
				}
				continue
			}
			seen[ref.ID] = true
			if skip != nil && skip(ref.ID) {
				continue
			}
			var m message
			if err := c.get(ctx, "/users/me/messages/"+url.PathEscape(ref.ID), url.Values{"format": {"full"}}, &m); err != nil {
				return nil, err
			}
			msg, err := m.read()
			if err != nil {
				return nil, err
			}
			out = append(out, msg)
		}
		if page.Next == "" {
			return out, nil
		}
		q.Set("pageToken", page.Next)
	}
}

// get reads the API's response at path into out. A read refused for the rate limit waits
// rateWait and tries again, up to rateRetries times.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		err = c.do(req, out)
		if !errors.Is(err, errRateLimited) || attempt == rateRetries {
			return err
		}
		if err := c.sleep(ctx, rateWait); err != nil {
			return err
		}
	}
}

// sleep waits d, or until ctx is done.
func (c *Client) sleep(ctx context.Context, d time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Insert puts n in the inbox, unread, from and to the mailbox's own address, without sending
// it, dated when it arrives.
func (c *Client) Insert(ctx context.Context, n Notice) error {
	var profile struct {
		Email string `json:"emailAddress"`
	}
	if err := c.get(ctx, "/users/me/profile", url.Values{}, &profile); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"raw":      base64.URLEncoding.EncodeToString(compose(profile.Email, n, time.Now())),
		"labelIds": []string{"INBOX", "UNREAD"},
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/users/me/messages?internalDateSource=receivedTime", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, nil)
}

// compose is n as a message from and to addr: its subject encoded for any alphabet, its body
// in base64 lines. A notice with HTML is multipart/alternative, its plain text first.
func compose(addr string, n Notice, now time.Time) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\n",
		addr, addr, mime.QEncoding.Encode("utf-8", n.Subject), now.Format(time.RFC1123Z))
	if n.HTML == "" {
		b.WriteString("Content-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n")
		writeBase64(&b, n.Body)
		return b.Bytes()
	}
	mw := multipart.NewWriter(&b) // writes to b, which can't fail
	fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=%s\r\n\r\n", mw.Boundary())
	for _, p := range []struct{ contentType, body string }{{"text/plain", n.Body}, {"text/html", n.HTML}} {
		w, _ := mw.CreatePart(textproto.MIMEHeader{"Content-Type": {p.contentType + "; charset=UTF-8"}, "Content-Transfer-Encoding": {"base64"}})
		writeBase64(w, p.body)
	}
	mw.Close()
	return b.Bytes()
}

// writeBase64 writes s in base64 lines of 76 characters.
func writeBase64(w io.Writer, s string) {
	enc := base64.StdEncoding.EncodeToString([]byte(s))
	for len(enc) > 76 {
		io.WriteString(w, enc[:76]+"\r\n")
		enc = enc[76:]
	}
	io.WriteString(w, enc+"\r\n")
}

// do sends req and reads its response into out, unless out is nil. A failure reports the
// API's status and message, never the request; a rejected token, or one without the scope
// the request needs, says to run categorize auth.
func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // the URL adds nothing to the token's or the network's error
		}
		return fmt.Errorf("gmail: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrAuth
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
				Details []struct {
					Reason string `json:"reason"`
				} `json:"details"`
				Errors []struct {
					Reason string `json:"reason"`
				} `json:"errors"`
			} `json:"error"`
		}
		json.NewDecoder(resp.Body).Decode(&e)
		if resp.StatusCode == http.StatusForbidden {
			for _, d := range e.Error.Details {
				if d.Reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT" {
					return ErrAuth
				}
			}
			for _, d := range e.Error.Errors {
				if d.Reason == "insufficientPermissions" {
					return ErrAuth
				}
			}
			if rateLimited(e.Error.Message, e.Error.Details, e.Error.Errors) {
				return fmt.Errorf("gmail: %s: %s: %w", resp.Status, e.Error.Message, errRateLimited)
			}
		}
		return fmt.Errorf("gmail: %s: %s", resp.Status, e.Error.Message)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("gmail: %w", err)
	}
	return nil
}

// rateLimited reports whether a 403's message or reasons name Gmail's rate limit or quota.
func rateLimited(message string, reasons ...[]struct {
	Reason string `json:"reason"`
}) bool {
	if strings.Contains(message, "Quota exceeded") {
		return true
	}
	for _, rs := range reasons {
		for _, r := range rs {
			if r.Reason == "rateLimitExceeded" || r.Reason == "userRateLimitExceeded" {
				return true
			}
		}
	}
	return false
}

// read is m's arrival in UTC, its From and To addresses, its subject and its text.
func (m message) read() (Message, error) {
	text, err := m.Payload.text()
	if err != nil {
		return Message{}, err
	}
	return Message{ID: m.ID, Received: time.UnixMilli(m.InternalDate).UTC(), From: m.Payload.address("From"), To: m.Payload.address("To"),
		Subject: m.Payload.header("Subject"), Text: text}, nil
}

// address is the address in p's header name without its display name, else the header as it is.
func (p part) address(name string) string {
	v := p.header(name)
	if a, err := mail.ParseAddress(v); err == nil {
		return a.Address
	}
	return v
}

// header is the value of p's header name, empty when it has none.
func (p part) header(name string) string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

// text is the body of p's first text/plain part, else, when there is none or its body is
// blank, the text of its first text/html part.
func (p part) text() (string, error) {
	if plain := p.first("text/plain"); plain != nil {
		body, err := plain.decode()
		if err != nil || strings.TrimSpace(body) != "" {
			return body, err
		}
	}
	if h := p.first("text/html"); h != nil {
		body, err := h.decode()
		if err != nil {
			return "", err
		}
		return htmlText(body), nil
	}
	return "", nil
}

// first is the first part of type mime in p and its nested parts, depth-first.
func (p *part) first(mime string) *part {
	if strings.EqualFold(p.MimeType, mime) {
		return p
	}
	for i := range p.Parts {
		if f := p.Parts[i].first(mime); f != nil {
			return f
		}
	}
	return nil
}

// decode is p's base64url body, padded or not.
func (p *part) decode() (string, error) {
	b, err := base64.URLEncoding.DecodeString(p.Body.Data)
	if err != nil {
		if b, err = base64.RawURLEncoding.DecodeString(p.Body.Data); err != nil {
			return "", fmt.Errorf("gmail message body: %w", err)
		}
	}
	return string(b), nil
}

// htmlText is the words of doc's text nodes joined by single spaces, without its scripts and
// styles.
func htmlText(doc string) string {
	var words []string
	z := html.NewTokenizer(strings.NewReader(doc))
	skip := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			return strings.Join(words, " ")
		case html.StartTagToken:
			name, _ := z.TagName()
			skip = string(name) == "script" || string(name) == "style"
		case html.EndTagToken:
			skip = false
		case html.TextToken:
			if !skip {
				words = append(words, strings.Fields(string(z.Text()))...)
			}
		}
	}
}
