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

// Client reads the user's messages through an HTTP client that authorizes its requests.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// Message is one message's arrival, recipient and text.
type Message struct {
	ID       string
	Received time.Time
	To       string // the bare address
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
// page of ids at a time, each message in full.
func (c *Client) Messages(ctx context.Context, query string, from, to time.Time) ([]Message, error) {
	q := url.Values{
		"q":          {fmt.Sprintf("%s after:%d before:%d", query, from.Unix(), to.AddDate(0, 0, 1).Unix())},
		"maxResults": {strconv.Itoa(pageSize)},
	}
	var out []Message
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

// get reads the API's response at path into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	return c.do(req, out)
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

// read is m's arrival in UTC, its To address and its text.
func (m message) read() (Message, error) {
	text, err := m.Payload.text()
	if err != nil {
		return Message{}, err
	}
	return Message{ID: m.ID, Received: time.UnixMilli(m.InternalDate).UTC(), To: m.Payload.to(), Text: text}, nil
}

// to is the address in p's To header without its display name, else the header as it is.
func (p part) to() string {
	for _, h := range p.Headers {
		if strings.EqualFold(h.Name, "To") {
			if a, err := mail.ParseAddress(h.Value); err == nil {
				return a.Address
			}
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
