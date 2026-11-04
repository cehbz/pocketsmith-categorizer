package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/gcal"
)

// authCmd asks the user's consent to read their calendar and Grab receipts, through the
// browser and a loopback redirect, and writes the token to the calendar's token file,
// owner-only; runs after it are unattended.
func authCmd(ctx context.Context, args []string, out io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	configFile, _ := config.Paths(home)
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	configPath := fs.String("config", configFile, "config file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("auth: unexpected argument %q", fs.Arg(0))
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	c := cfg.Calendar
	conf, err := googleOAuth(ctx, c)
	if err != nil {
		return err
	}
	t, err := gcal.Consent(ctx, conf, func(url string) {
		fmt.Fprintf(out, "Open this page to let the categorizer read your calendar and Grab receipts:\n\n  %s\n\nWaiting for your consent...\n", url)
	})
	if err != nil {
		return err
	}
	if err := gcal.TokenFile(c.TokenRef).Save(t); err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "token written to %s\n", c.TokenRef)
	return err
}
