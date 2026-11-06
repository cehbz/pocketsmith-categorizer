package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cehbz/pocketsmith-categorizer/config"
	"github.com/cehbz/pocketsmith-categorizer/leak"
	"github.com/cehbz/pocketsmith-categorizer/memo"
)

const dictionaryPath = "/usr/share/dict/words"

// leakCheckCmd is the repository's commit-msg hook: it refuses a commit whose message or staged
// lines add a word from a real payee.
func leakCheckCmd(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: categorize leak-check <commit message file>")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return leakCheck(ctx, args[0], dictionaryPath, config.PayeesPath(home), config.PublicWordsPath(home))
}

func leakCheck(ctx context.Context, messagePath, dictionaryPath, payeesPath, publicPath string) error {
	payees, err := memo.Load(payeesPath)
	if err != nil {
		return err
	}
	if len(payees.Keys()) == 0 { // an empty list would pass everything
		return fmt.Errorf("leak-check: no payees in %s; a categorize run writes them", payeesPath)
	}
	dictionary, err := os.ReadFile(dictionaryPath)
	if err != nil {
		return fmt.Errorf("leak-check: %w", err)
	}
	public, err := os.ReadFile(publicPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("leak-check: %w", err)
	}
	message, err := os.ReadFile(messagePath)
	if err != nil {
		return fmt.Errorf("leak-check: %w", err)
	}
	staged, err := gitOutput(ctx, "diff", "--cached", "-U0", "--no-color", "--no-ext-diff")
	if err != nil {
		return err
	}
	tracked, err := gitOutput(ctx, "grep", "-hIe", "", "HEAD", "--", ".")
	if err != nil {
		return err
	}
	private := leak.NewPrivate(payees.Keys(), append(strings.Fields(string(dictionary)), strings.Fields(string(public))...))
	found := private.In(string(message)+"\n"+addedLines(staged), tracked)
	if len(found) > 0 {
		return fmt.Errorf("leak-check: this commit adds words from real payees: %s\nreplace them with made-up values, or add a word that's safe to publish to %s",
			strings.Join(found, ", "), publicPath)
	}
	return nil
}

// addedLines keeps the lines a diff adds, without their markers.
func addedLines(diff string) string {
	var added []string
	for line := range strings.Lines(diff) {
		if text, ok := strings.CutPrefix(line, "+"); ok && !strings.HasPrefix(text, "++ ") {
			added = append(added, text)
		}
	}
	return strings.Join(added, "")
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return "", fmt.Errorf("leak-check: git %s: %w", args[0], err)
	}
	return string(out), nil
}
