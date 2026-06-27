package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/reinanbr/auto_pull_go/autopull"
)

// ─── BackoffDuration ─────────────────────────

func TestBackoffDuration_ZeroFailures(t *testing.T) {
	if d := autopull.BackoffDuration(0); d != 0 {
		t.Errorf("expected 0, got %v", d)
	}
}

func TestBackoffDuration_Exponential(t *testing.T) {
	cases := []struct {
		failures int
		want     time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{4, 8 * time.Second},
		{5, 16 * time.Second},
	}
	for _, c := range cases {
		got := autopull.BackoffDuration(c.failures)
		if got != c.want {
			t.Errorf("BackoffDuration(%d) = %v, want %v", c.failures, got, c.want)
		}
	}
}

func TestBackoffDuration_CappedAt5Min(t *testing.T) {
	max := 5 * time.Minute
	for _, n := range []int{10, 50, 100, 1000} {
		got := autopull.BackoffDuration(n)
		if got != max {
			t.Errorf("BackoffDuration(%d) = %v, want %v (cap)", n, got, max)
		}
	}
}

func TestBackoffDuration_NoOverflow(t *testing.T) {
	// must not panic for very large failure counts
	_ = autopull.BackoffDuration(1 << 20)
}

// ─── TailFile ────────────────────────────────

func TestTailFile_LastNLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	content := "line1\nline2\nline3\nline4\nline5\n"
	os.WriteFile(path, []byte(content), 0644)

	got, err := autopull.TailFile(path, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "line3\nline4\nline5" {
		t.Errorf("got %q, want line3..line5", got)
	}
}

func TestTailFile_FewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, []byte("only one line\n"), 0644)

	got, err := autopull.TailFile(path, 50)
	if err != nil || got != "only one line" {
		t.Errorf("got %q %v, want 'only one line' nil", got, err)
	}
}

func TestTailFile_ExactLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, []byte("a\nb\nc\n"), 0644)

	got, err := autopull.TailFile(path, 3)
	if err != nil || got != "a\nb\nc" {
		t.Errorf("got %q %v, want 'a\\nb\\nc' nil", got, err)
	}
}

func TestTailFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.log")
	os.WriteFile(path, []byte(""), 0644)

	got, err := autopull.TailFile(path, 10)
	if err != nil || got != "" {
		t.Errorf("got %q %v, want '' nil", got, err)
	}
}

func TestTailFile_MissingFile(t *testing.T) {
	_, err := autopull.TailFile("/nonexistent/file.log", 10)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

// ─── RotateIfLarge ───────────────────────────

func TestRotateIfLarge_NoRotationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, []byte("small"), 0644)

	if err := autopull.RotateIfLarge(path, 1024*1024); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("original file should still exist")
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("backup should not exist when rotation not needed")
	}
}

func TestRotateIfLarge_RotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	os.WriteFile(path, []byte("0123456789"), 0644)

	if err := autopull.RotateIfLarge(path, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path + ".1"); err != nil {
		t.Error("backup should exist after rotation")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("original should be gone after rotation")
	}
}

func TestRotateIfLarge_MissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.log")
	if err := autopull.RotateIfLarge(path, 1024); err != nil {
		t.Errorf("expected no error for missing file, got %v", err)
	}
}

func TestRotateIfLarge_PreviousBackupOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	backup := path + ".1"
	os.WriteFile(path, []byte("0123456789"), 0644)
	os.WriteFile(backup, []byte("old"), 0644)

	if err := autopull.RotateIfLarge(path, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, _ := os.ReadFile(backup)
	if strings.Contains(string(data), "old") {
		t.Error("old backup should be overwritten")
	}
}
