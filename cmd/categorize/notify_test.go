package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/categorize"
	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/gcal"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/report"
)

// fakeInbox records the notices inserted into it.
type fakeInbox struct {
	notices []gmail.Notice
	err     error
}

func (b *fakeInbox) Insert(_ context.Context, n gmail.Notice) error {
	b.notices = append(b.notices, n)
	return b.err
}

func newNotifier(t *testing.T) (*notifier, *fakeInbox) {
	t.Helper()
	b := &fakeInbox{}
	return &notifier{inbox: b, dir: filepath.Join(t.TempDir(), "reports"), start: time.Date(2026, 11, 5, 21, 0, 0, 0, time.UTC)}, b
}

func TestAScheduledRunSavesItsReportUnderItsRunID(t *testing.T) {
	n, _ := newNotifier(t)
	var out strings.Builder
	if err := n.report(context.Background(), &out, report.Run{RunID: "20261105T210000.000000000Z"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(n.dir, "20261105T210000.000000000Z.txt"))
	if err != nil || string(b) != out.String() || !strings.HasPrefix(string(b), "Run ") {
		t.Errorf("saved %q, %v; want the report the log got, %q", b, err, out.String())
	}
	if info, err := os.Stat(filepath.Join(n.dir, "20261105T210000.000000000Z.txt")); err != nil {
		t.Error(err)
	} else if info.Mode().Perm() != 0o600 {
		t.Errorf("report file mode %v; want 0600", info.Mode().Perm())
	}
}

func TestARunWithoutARunIDSavesItsReportUnderItsStartTime(t *testing.T) {
	n, _ := newNotifier(t)
	var out strings.Builder
	if err := n.report(context.Background(), &out, report.Run{RunID: "(none)"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(n.dir, journal.ID(n.start)+".txt")); err != nil {
		t.Errorf("report under the start time: %v", err)
	}
}

func TestAReportWithSomethingNewInsertsOneNoticeCarryingIt(t *testing.T) {
	n, b := newNotifier(t)
	var out strings.Builder
	r := report.Run{RunID: "(none)", Flags: []categorize.Change{{Charge: 1}}, FlagsApplied: 1}
	if err := n.report(context.Background(), &out, r); err != nil {
		t.Fatal(err)
	}
	var page strings.Builder
	if err := report.WriteRunHTML(&page, r); err != nil {
		t.Fatal(err)
	}
	if len(b.notices) != 1 || b.notices[0].Subject != "Categorizer: 1 transaction to review" || b.notices[0].Body != out.String() || b.notices[0].HTML != page.String() {
		t.Errorf("notices %+v; want one, subject Categorizer: 1 transaction to review, body the report, HTML its page", b.notices)
	}
}

func TestAReportWithNothingNewInsertsNoNotice(t *testing.T) {
	n, b := newNotifier(t)
	var out strings.Builder
	if err := n.report(context.Background(), &out, report.Run{RunID: "(none)"}); err != nil {
		t.Fatal(err)
	}
	if len(b.notices) != 0 {
		t.Errorf("notices %+v; want none", b.notices)
	}
}

func TestAFailedRunInsertsAFailureNoticeNamingTheError(t *testing.T) {
	n, b := newNotifier(t)
	runErr := errors.New("pocketsmith: GET /me: context deadline exceeded")
	if err := notified(context.Background(), n, runErr); !errors.Is(err, runErr) {
		t.Errorf("notified: %v; want the run's error", err)
	}
	if len(b.notices) != 1 || b.notices[0].Subject != "categorize run failed: pocketsmith: GET /me: context deadline exceeded" || !strings.Contains(b.notices[0].Body, runErr.Error()) || b.notices[0].HTML != "" {
		t.Errorf("notices %+v; want one plain-text failure notice naming the error", b.notices)
	}
	if err := notified(context.Background(), n, nil); err != nil || len(b.notices) != 1 {
		t.Errorf("notified(nil): %v with %d notices; want nil and no new notice", err, len(b.notices))
	}
}

func TestAnInsertFailureFailsTheReport(t *testing.T) {
	n, b := newNotifier(t)
	b.err = gmail.ErrAuth
	var out strings.Builder
	r := report.Run{RunID: "(none)", Flags: []categorize.Change{{Charge: 1}}, FlagsApplied: 1}
	if err := n.report(context.Background(), &out, r); !errors.Is(err, gmail.ErrAuth) {
		t.Errorf("report: %v; want the insert's error", err)
	}
}

func TestGoogleOAuthAsksForCalendarGmailReadAndGmailInsert(t *testing.T) {
	dir := t.TempDir()
	id, secret := filepath.Join(dir, "id"), filepath.Join(dir, "secret")
	for _, p := range []string{id, secret} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	conf, err := googleOAuth(context.Background(), config.Calendar{ID: "primary", ClientIDRef: id, ClientSecretRef: secret, TokenRef: filepath.Join(dir, "token.json")})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{gcal.Scope, gmail.Scope, gmail.InsertScope}; !reflect.DeepEqual(conf.Scopes, want) {
		t.Errorf("scopes %v; want %v", conf.Scopes, want)
	}
}
