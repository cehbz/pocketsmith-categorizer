package gcal

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// tokens is a fake token endpoint answering status with body, recording each request's form.
func tokens(t *testing.T, status int, body string) (conf *oauth2.Config, forms *[]url.Values) {
	t.Helper()
	forms = &[]url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		*forms = append(*forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	conf = OAuth("client-1", "s3cret", Scope)
	conf.Endpoint = oauth2.Endpoint{AuthURL: srv.URL + "/auth", TokenURL: srv.URL + "/token", AuthStyle: oauth2.AuthStyleInParams}
	return conf, forms
}

func TestOAuthAsksForTheScopesGiven(t *testing.T) {
	if got := OAuth("id", "secret", "a", "b").Scopes; !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("scopes = %q, want [a b]", got)
	}
}

// tokenFile is a token file holding t.
func tokenFile(t *testing.T, tok *oauth2.Token) TokenFile {
	t.Helper()
	f := TokenFile(filepath.Join(t.TempDir(), "token.json"))
	if err := f.Save(tok); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestTheTokenFileRoundTripsOwnerOnlyAndRefusesOneOthersCanRead(t *testing.T) {
	f := tokenFile(t, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1"})
	info, err := os.Stat(string(f))
	if err != nil {
		t.Fatalf("token file not written: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("token file mode %v, want 0600", info.Mode())
	}
	got, err := f.Load()
	if err != nil || got.AccessToken != "a1" || got.RefreshToken != "r1" {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	os.Chmod(string(f), 0o644)
	if _, err := f.Load(); err == nil || strings.Contains(err.Error(), "r1") {
		t.Errorf("Load of a file others can read: %v; want an error without the token", err)
	}
	if _, err := TokenFile(filepath.Join(t.TempDir(), "none")).Load(); !errors.Is(err, ErrAuth) {
		t.Errorf("Load of a missing file: %v; want ErrAuth", err)
	}
}

func TestAnExpiredTokenIsRefreshedAndRewritten(t *testing.T) {
	conf, forms := tokens(t, http.StatusOK, `{"access_token":"a2","token_type":"Bearer","expires_in":3600}`)
	f := tokenFile(t, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1", Expiry: time.Now().Add(-time.Hour)})
	srv, queries := calendar(t, []map[string]any{{"summary": "Dinner", "start": map[string]string{"date": "2026-03-12"}, "end": map[string]string{"date": "2026-03-13"}}})
	h, err := Authorized(context.Background(), conf, f, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (&Client{BaseURL: srv.URL, ID: "cal@example.com", HTTP: h}).Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31")); err != nil {
		t.Fatal(err)
	}
	if len(*forms) != 1 || (*forms)[0].Get("grant_type") != "refresh_token" || (*forms)[0].Get("refresh_token") != "r1" {
		t.Errorf("token requests %v; want one refresh with r1", *forms)
	}
	if len(*queries) != 1 || (*queries)[0]["auth"] != "Bearer a2" {
		t.Errorf("calendar requests %v; want one authorized with the refreshed token", *queries)
	}
	saved, err := f.Load()
	if err != nil || saved.AccessToken != "a2" || saved.RefreshToken != "r1" {
		t.Errorf("token file holds %+v, %v; want a2 with the refresh token kept", saved, err)
	}
}

func TestAnExpiredTokenWithoutARefreshTokenSaysToRunAuth(t *testing.T) {
	conf, _ := tokens(t, http.StatusOK, `{}`)
	f := tokenFile(t, &oauth2.Token{AccessToken: "a1", Expiry: time.Now().Add(-time.Hour)})
	if _, err := Authorized(context.Background(), conf, f, http.DefaultClient); !errors.Is(err, ErrAuth) || !strings.Contains(err.Error(), "categorize auth") {
		t.Errorf("err = %v; want ErrAuth", err)
	}
}

func TestARevokedTokenSaysToRunAuthWithoutItsSecrets(t *testing.T) {
	conf, _ := tokens(t, http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
	f := tokenFile(t, &oauth2.Token{AccessToken: "a1", RefreshToken: "r1", Expiry: time.Now().Add(-time.Hour)})
	srv, _ := calendar(t, nil)
	h, err := Authorized(context.Background(), conf, f, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	_, err = (&Client{BaseURL: srv.URL, ID: "cal@example.com", HTTP: h}).Events(context.Background(), mustDay("2026-03-01"), mustDay("2026-03-31"))
	if !errors.Is(err, ErrAuth) || strings.Contains(err.Error(), "s3cret") || strings.Contains(err.Error(), "r1") {
		t.Errorf("err = %v; want ErrAuth without the client secret or refresh token", err)
	}
}

// browser follows the consent page to its redirect with code, giving back the state it was
// given, or state when it isn't empty.
func browser(t *testing.T, code, state string) func(string) {
	return func(page string) {
		u, err := url.Parse(page)
		if err != nil {
			t.Error(err)
			return
		}
		q := u.Query()
		if q.Get("scope") != Scope || q.Get("access_type") != "offline" || q.Get("code_challenge") == "" {
			t.Errorf("consent page %s; want the calendar scope, offline access and a code challenge", page)
		}
		if state == "" {
			state = q.Get("state")
		}
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"code": {code}, "state": {state}}.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
}

func TestConsentExchangesTheLoopbackCodeForARefreshableToken(t *testing.T) {
	conf, forms := tokens(t, http.StatusOK, `{"access_token":"a1","refresh_token":"r1","token_type":"Bearer","expires_in":3600}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := Consent(ctx, conf, browser(t, "c0de", ""))
	if err != nil {
		t.Fatal(err)
	}
	if got.RefreshToken != "r1" || len(*forms) != 1 {
		t.Fatalf("token %+v after %d exchanges; want refresh token r1 from one", got, len(*forms))
	}
	f := (*forms)[0]
	if f.Get("code") != "c0de" || f.Get("code_verifier") == "" || !strings.HasPrefix(f.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Errorf("exchange %v; want the code, a verifier and the loopback redirect", f)
	}
}

func TestConsentRefusesACallbackWithAnotherState(t *testing.T) {
	conf, forms := tokens(t, http.StatusOK, `{"access_token":"a1","refresh_token":"r1","token_type":"Bearer"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Consent(ctx, conf, browser(t, "c0de", "forged")); err == nil || !strings.Contains(err.Error(), "state") || len(*forms) != 0 {
		t.Errorf("err = %v, %d exchanges; want a state mismatch and no exchange", err, len(*forms))
	}
}

func TestDecliningConsentEndsAuthWithGooglesError(t *testing.T) {
	conf, forms := tokens(t, http.StatusOK, `{"access_token":"a1","refresh_token":"r1","token_type":"Bearer"}`)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	declined := func(page string) {
		u, _ := url.Parse(page)
		q := u.Query()
		go func() {
			resp, err := http.Get(q.Get("redirect_uri") + "?" + url.Values{"error": {"access_denied"}, "state": {q.Get("state")}}.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	if _, err := Consent(ctx, conf, declined); err == nil || !strings.Contains(err.Error(), "access_denied") || len(*forms) != 0 {
		t.Errorf("err = %v, %d exchanges; want Google's access_denied and no exchange", err, len(*forms))
	}
}

func TestATokenIsSavedWhereItsDirectoryDoesntExistYet(t *testing.T) {
	f := TokenFile(filepath.Join(t.TempDir(), "secrets", "token.json"))
	if err := f.Save(&oauth2.Token{AccessToken: "a1", RefreshToken: "r1"}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.Load(); err != nil || got.RefreshToken != "r1" {
		t.Errorf("Load = %+v, %v", got, err)
	}
}
