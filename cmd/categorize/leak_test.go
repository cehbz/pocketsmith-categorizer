package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// leakRepo makes a repository with one commit tracking Knownword and a payee list, and
// returns a check of the staged changes under a commit message.
func leakRepo(t *testing.T, payees ...string) (dir string, check func(message string) error) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, text string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write(filepath.Join(dir, "a.txt"), "Knownword\n")
	git("add", "a.txt")
	git("commit", "-qm", "first")
	state := t.TempDir()
	list, err := json.Marshal(payees)
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(state, "payees.json"), string(list))
	write(filepath.Join(state, "words"), "ordinary\n")
	write(filepath.Join(state, "public.txt"), "PUBLICWORD\n")
	t.Chdir(dir)
	return dir, func(message string) error {
		t.Helper()
		write(filepath.Join(state, "message"), message)
		return leakCheck(context.Background(), filepath.Join(state, "message"), filepath.Join(state, "words"), filepath.Join(state, "payees.json"), filepath.Join(state, "public.txt"))
	}
}

func stage(t *testing.T, dir, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "b.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
}

func TestLeakCheckRefusesAPayeeWordInAStagedLine(t *testing.T) {
	dir, check := leakRepo(t, "ZELLE TO QUIMBLE")
	stage(t, dir, "fixture quimble\n")
	if err := check("Add a test"); err == nil || !strings.Contains(err.Error(), "QUIMBLE") {
		t.Errorf("check = %v, want it to name QUIMBLE", err)
	}
}

func TestLeakCheckRefusesAPayeeWordInTheMessage(t *testing.T) {
	_, check := leakRepo(t, "STAYS * HQ7XK2PLM")
	if err := check("Fix the rule for HQ7XK2PLM"); err == nil || !strings.Contains(err.Error(), "HQ7XK2PLM") {
		t.Errorf("check = %v, want it to name HQ7XK2PLM", err)
	}
}

func TestLeakCheckPassesWordsThatArePublicOrTrackedOrOrdinary(t *testing.T) {
	dir, check := leakRepo(t, "PUBLICWORD ROAD", "KNOWNWORD", "ORDINARY", "QUIMBLE")
	stage(t, dir, "publicword knownword ordinary\n")
	if err := check("Add a test\n\nOrdinary words."); err != nil {
		t.Errorf("check = %v, want none", err)
	}
}

func TestLeakCheckFailsWithoutAPayeeList(t *testing.T) {
	_, check := leakRepo(t)
	if err := check("Add a test"); err == nil {
		t.Error("check passed with an empty payee list")
	}
}
