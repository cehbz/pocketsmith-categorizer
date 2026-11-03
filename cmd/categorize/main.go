// Command categorize decides PocketSmith categories through the categorizer's layers:
// "categorize run" applies decisions, "categorize replay" measures rules against the user's
// categorizations, "categorize undo [run-id]" reverses a run, "categorize auth" lets it read
// the user's calendar.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "categorize:", err)
		os.Exit(1)
	}
}

const usage = "usage: categorize run|replay|undo|auth [flags]"

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "run":
		return runCmd(ctx, args[1:], out)
	case "replay":
		return replayCmd(ctx, args[1:], out)
	case "undo":
		return undoCmd(ctx, args[1:], out)
	case "auth":
		return authCmd(ctx, args[1:], out)
	}
	return errors.New(usage)
}
