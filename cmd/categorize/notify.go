package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/gcal"
	"github.com/cehbz/pocketsmith-categorizer/gmail"
	"github.com/cehbz/pocketsmith-categorizer/journal"
	"github.com/cehbz/pocketsmith-categorizer/report"
	"github.com/cehbz/pocketsmith-categorizer/retry"
)

// inbox takes notices for the user.
type inbox interface {
	Insert(ctx context.Context, n gmail.Notice) error
}

// notifier saves a scheduled run's report and puts a notice in the user's inbox when the run
// needs them or failed.
type notifier struct {
	inbox inbox
	dir   string    // where reports are saved
	start time.Time // the run's start, naming a report without a run ID
}

// openNotifier is the notifier for the Gmail account the config's Google token opens, built
// before the session so a session that fails to open is still reported.
func openNotifier(ctx context.Context, configPath, home string) (*notifier, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	conf, err := googleOAuth(ctx, cfg.Calendar)
	if err != nil {
		return nil, err
	}
	h, err := gcal.Authorized(ctx, conf, gcal.TokenFile(cfg.Calendar.TokenRef), retry.Client(60*time.Second))
	if err != nil {
		return nil, err
	}
	return &notifier{inbox: &gmail.Client{BaseURL: gmail.BaseURL, HTTP: h}, dir: config.ReportsDir(home), start: time.Now()}, nil
}

// report saves r, copies it to out, and puts it in the inbox when it has something new for
// the user: the text report as the plain part, its HTML page as the other.
func (n *notifier) report(ctx context.Context, out io.Writer, r report.Run) error {
	var b bytes.Buffer
	if err := report.WriteRun(&b, r); err != nil {
		return err
	}
	name := r.RunID
	if name == "(none)" {
		name = journal.ID(n.start)
	}
	if err := os.MkdirAll(n.dir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(n.dir, name+".txt"), b.Bytes(), 0o600); err != nil {
		return err
	}
	if _, err := out.Write(b.Bytes()); err != nil {
		return err
	}
	subject, due := report.Notice(r)
	if !due {
		return nil
	}
	var page strings.Builder
	if err := report.WriteRunHTML(&page, r); err != nil {
		return err
	}
	return n.inbox.Insert(ctx, gmail.Notice{Subject: subject, Body: b.String(), HTML: page.String()})
}

// failed puts a notice of a run that failed with err in the inbox.
func (n *notifier) failed(ctx context.Context, err error) error {
	return n.inbox.Insert(ctx, gmail.Notice{Subject: report.Failure(err), Body: err.Error() + "\n"})
}

// notified is a run's error, joined with any failure to report it.
func notified(ctx context.Context, n *notifier, err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(err, n.failed(ctx, err))
}
