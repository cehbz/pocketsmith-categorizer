package memo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAMissingFileIsEmpty(t *testing.T) {
	m, err := Load(filepath.Join(t.TempDir(), "state", "shown.json"))
	if err != nil || m.Seen("a") {
		t.Errorf("Load = %v, %v", m, err)
	}
}

func TestAddedKeysSurviveASave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "shown.json")
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m.Add("b")
	m.Add("a")
	if !m.Seen("a") {
		t.Error("an added key is not seen before saving")
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	again, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Seen("a") || !again.Seen("b") || again.Seen("c") {
		t.Errorf("reloaded memo = %v", again)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, %v, want 0600", info.Mode().Perm(), err)
	}
}

func TestACorruptFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "shown.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load accepted a corrupt file")
	}
}
