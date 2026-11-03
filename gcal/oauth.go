package gcal

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"

	"github.com/cehbz/pocketsmith-categorizer/config"
)

// Scope reads calendars and nothing else.
const Scope = "https://www.googleapis.com/auth/calendar.readonly"

// ErrAuth is a token that is missing, expired without a way to refresh it, or revoked.
var ErrAuth = errors.New("calendar: no usable token: run categorize auth")

// OAuth is the OAuth client for reading Google calendars.
func OAuth(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{ClientID: clientID, ClientSecret: clientSecret, Endpoint: endpoints.Google, Scopes: []string{Scope}}
}

// TokenFile is an OAuth token in a file only its owner can read.
type TokenFile string

// Load reads the token; a missing file is ErrAuth.
func (f TokenFile) Load() (*oauth2.Token, error) {
	b, err := config.ReadOwnerOnly(string(f))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w (%s is missing)", ErrAuth, f)
	}
	if err != nil {
		return nil, fmt.Errorf("calendar token: %w", err)
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("calendar token %s: not a token", f)
	}
	return &t, nil
}

// Save replaces the token atomically, owner-only.
func (f TokenFile) Save(t *oauth2.Token) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(string(f))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), string(f))
}

// Authorized is an HTTP client like base whose requests carry the token in file, refreshed
// through conf when it expires; a refreshed token is written back to the file.
func Authorized(ctx context.Context, conf *oauth2.Config, file TokenFile, base *http.Client) (*http.Client, error) {
	t, err := file.Load()
	if err != nil {
		return nil, err
	}
	if !t.Valid() && t.RefreshToken == "" {
		return nil, ErrAuth
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	return oauth2.NewClient(ctx, &saving{src: conf.TokenSource(ctx, t), file: file, last: t.AccessToken}), nil
}

// saving is a token source that writes each new token to its file. oauth2's client asks it
// for a token only when the one it holds has expired, one request at a time.
type saving struct {
	src  oauth2.TokenSource
	file TokenFile
	last string // the access token in the file
}

func (s *saving) Token() (*oauth2.Token, error) {
	t, err := s.src.Token()
	if err != nil {
		return nil, tokenError("token refresh", err)
	}
	if t.AccessToken != s.last {
		if err := s.file.Save(t); err != nil {
			return nil, fmt.Errorf("calendar token: %w", err)
		}
		s.last = t.AccessToken
	}
	return t, nil
}

// tokenError is the error of a token request: a revoked or expired grant is ErrAuth; any other
// refusal names its error code or status, never the response, which echoes the request.
func tokenError(op string, err error) error {
	var re *oauth2.RetrieveError
	switch {
	case errors.As(err, &re) && re.ErrorCode == "invalid_grant":
		return ErrAuth
	case errors.As(err, &re):
		return fmt.Errorf("calendar %s: %s", op, cmp.Or(re.ErrorCode, re.Response.Status))
	}
	return fmt.Errorf("calendar %s: %w", op, err)
}

// Consent asks the user's consent through the browser: show is given the consent page's URL,
// and Google sends the code to a listener on the loopback interface, which exchanges it for a
// token that refreshes itself.
func Consent(ctx context.Context, conf *oauth2.Config, show func(url string)) (*oauth2.Token, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := *conf
	c.RedirectURL = "http://" + l.Addr().String() + "/callback"
	state, verifier := rand.Text(), oauth2.GenerateVerifier()
	codes, errs := make(chan string, 1), make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.URL.Path != "/callback":
			http.NotFound(w, r)
		case q.Get("state") != state:
			http.Error(w, "state mismatch", http.StatusBadRequest)
			sendErr(errs, errors.New("calendar consent: state mismatch"))
		case q.Get("code") == "":
			http.Error(w, "no code", http.StatusBadRequest)
			sendErr(errs, fmt.Errorf("calendar consent: %s", cmp.Or(q.Get("error"), "no code")))
		default:
			fmt.Fprintln(w, "Authorized. You can close this tab.")
			select {
			case codes <- q.Get("code"):
			default:
			}
		}
	})}
	go srv.Serve(l)
	defer srv.Close()
	show(c.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier)))
	select {
	case code := <-codes:
		t, err := c.Exchange(ctx, code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, tokenError("consent", err)
		}
		return t, nil
	case err := <-errs:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func sendErr(errs chan<- error, err error) {
	select {
	case errs <- err:
	default:
	}
}
