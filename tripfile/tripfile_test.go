package tripfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cehbz/pocketsmith-categorizer/internal/day"
	"github.com/cehbz/pocketsmith-categorizer/trips"
)

func date(s string) time.Time {
	d, err := day.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

func file(t *testing.T) File {
	dir := t.TempDir()
	return File{List: filepath.Join(dir, "config", "trips.toml"), Written: filepath.Join(dir, "state", "trips-written.toml")}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	f := file(t)
	list := trips.List{{Name: "trip-2026-03-10", From: date("2026-03-10"), To: date("2026-03-14"), VisaRun: true}}
	written := []trips.Trip{list[0], {Name: "trip-2026-05-01", From: date("2026-05-01"), To: date("2026-05-03")}}
	if err := f.Save(list, written); err != nil {
		t.Fatal(err)
	}
	gotList, gotWritten, err := f.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotList, list) || !reflect.DeepEqual(gotWritten, written) {
		t.Errorf("Load = %+v, %+v; want %+v, %+v", gotList, gotWritten, list, written)
	}
	for _, path := range []string{f.List, f.Written} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v; want 0600", path, info.Mode().Perm(), err)
		}
	}
	if b, _ := os.ReadFile(f.List); !strings.HasPrefix(string(b), "# Trips") {
		t.Errorf("the list doesn't start with its header: %q", b)
	}
}

func TestMissingFilesAreEmpty(t *testing.T) {
	list, written, err := file(t).Load()
	if err != nil || len(list) != 0 || len(written) != 0 {
		t.Errorf("Load = %v, %v, %v; want empty", list, written, err)
	}
}

func TestLoadRejectsBadTrips(t *testing.T) {
	for name, content := range map[string]string{
		"space in name": "[[trip]]\nname = \"my trip\"\nfrom = \"2026-03-10\"\nto = \"2026-03-12\"\n",
		"no name":       "[[trip]]\nfrom = \"2026-03-10\"\nto = \"2026-03-12\"\n",
		"bad date":      "[[trip]]\nname = \"t\"\nfrom = \"2026-3-10\"\nto = \"2026-03-12\"\n",
		"backwards":     "[[trip]]\nname = \"t\"\nfrom = \"2026-03-12\"\nto = \"2026-03-10\"\n",
		"unknown key":   "[[trip]]\nname = \"t\"\nfrom = \"2026-03-10\"\nto = \"2026-03-12\"\nvisarun = true\n",
	} {
		f := file(t)
		if err := os.MkdirAll(filepath.Dir(f.List), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f.List, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.Load(); err == nil || !strings.Contains(err.Error(), f.List) {
			t.Errorf("%s: Load error = %v, want one naming the file", name, err)
		}
	}
}
