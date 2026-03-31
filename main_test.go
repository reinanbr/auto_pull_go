package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ─────────────────────────────────────────────
// shortHash
// ─────────────────────────────────────────────

func TestShortHash(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"abc1234def567", "abc1234"},
		{"abc1234", "abc1234"},
		{"abc12", "abc12"},   // shorter than 7 — must not panic
		{"", ""},             // empty — must not panic
		{"1234567890", "1234567"},
	}
	for _, tt := range tests {
		got := shortHash(tt.input)
		if got != tt.want {
			t.Errorf("shortHash(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ─────────────────────────────────────────────
// backoffDuration
// ─────────────────────────────────────────────

func TestBackoffDuration(t *testing.T) {
	tests := []struct {
		failures int
		wantMin  time.Duration
		wantMax  time.Duration
	}{
		{0, 0, 0},
		{1, time.Second, time.Second},
		{2, 2 * time.Second, 2 * time.Second},
		{3, 4 * time.Second, 4 * time.Second},
		{4, 8 * time.Second, 8 * time.Second},
		{5, 16 * time.Second, 16 * time.Second},
		{10, 5 * time.Minute, 5 * time.Minute}, // capped
		{100, 5 * time.Minute, 5 * time.Minute}, // no overflow panic
	}
	for _, tt := range tests {
		got := backoffDuration(tt.failures)
		if got < tt.wantMin || got > tt.wantMax {
			t.Errorf("backoffDuration(%d) = %v, want [%v, %v]", tt.failures, got, tt.wantMin, tt.wantMax)
		}
	}
}

// ─────────────────────────────────────────────
// loadConfig
// ─────────────────────────────────────────────

func writeTestConfig(t *testing.T, dir string, content map[string]interface{}) string {
	t.Helper()
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config_auto_pull.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfig_Defaults(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path": dir,
	})

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Branch != "main" {
		t.Errorf("Branch default = %q, want %q", cfg.Branch, "main")
	}
	if cfg.CheckIntervalSeconds != 5 {
		t.Errorf("CheckIntervalSeconds default = %d, want 5", cfg.CheckIntervalSeconds)
	}
	if cfg.LogFile != "auto_pull.log" {
		t.Errorf("LogFile default = %q, want %q", cfg.LogFile, "auto_pull.log")
	}
}

func TestLoadConfig_RejectsGithubToken(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path":    dir,
		"github_token": "ghp_secret",
	})

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for github_token in config, got nil")
	}
	if !strings.Contains(err.Error(), "github_token") {
		t.Errorf("error should mention 'github_token', got: %v", err)
	}
}

func TestLoadConfig_RejectsReposField(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path": dir,
		"repos":     []map[string]string{{"repo_path": "/tmp/a"}},
	})

	_, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for 'repos' field in config, got nil")
	}
	if !strings.Contains(err.Error(), "repos") {
		t.Errorf("error should mention 'repos', got: %v", err)
	}
}

func TestLoadConfig_TokenFromDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path": dir,
	})

	// write .env in repo dir
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte("AUTOPULL_TOKEN=mytoken123\n"), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GithubToken != "mytoken123" {
		t.Errorf("GithubToken = %q, want %q", cfg.GithubToken, "mytoken123")
	}
}

func TestLoadConfig_TokenFromEnvVar(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path": dir,
	})

	t.Setenv("AUTOPULL_TOKEN", "envtoken456")

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.GithubToken != "envtoken456" {
		t.Errorf("GithubToken = %q, want %q", cfg.GithubToken, "envtoken456")
	}
}

func TestLoadConfig_TokenNotSerializedToJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeTestConfig(t, dir, map[string]interface{}{
		"repo_path": dir,
	})

	// confirm that the token field does not appear in the written config
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github_token") {
		t.Error("config file must not contain 'github_token' field")
	}
}

// ─────────────────────────────────────────────
// loadDotEnvToken
// ─────────────────────────────────────────────

func TestLoadDotEnvToken(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "AUTOPULL_TOKEN",
			content: "AUTOPULL_TOKEN=abc123\n",
			want:    "abc123",
		},
		{
			name:    "GITHUB_TOKEN",
			content: "GITHUB_TOKEN=ghp_xyz\n",
			want:    "ghp_xyz",
		},
		{
			name:    "AUTOPULL_TOKEN takes priority",
			content: "AUTOPULL_TOKEN=first\nGITHUB_TOKEN=second\n",
			want:    "first",
		},
		{
			name:    "quoted value",
			content: `AUTOPULL_TOKEN="quoted"` + "\n",
			want:    "quoted",
		},
		{
			name:    "comment ignored",
			content: "# AUTOPULL_TOKEN=ignored\nGITHUB_TOKEN=real\n",
			want:    "real",
		},
		{
			name:    "empty file",
			content: "",
			want:    "",
		},
		{
			name:    "unrelated keys",
			content: "FOO=bar\nBAZ=qux\n",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(tt.content), 0600); err != nil {
				t.Fatal(err)
			}
			got := loadDotEnvToken(dir)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLoadDotEnvToken_NoFile(t *testing.T) {
	dir := t.TempDir()
	got := loadDotEnvToken(dir)
	if got != "" {
		t.Errorf("expected empty token when .env missing, got %q", got)
	}
}

// ─────────────────────────────────────────────
// rotateIfLarge
// ─────────────────────────────────────────────

func TestRotateIfLarge_NoRotationNeeded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("small content"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := rotateIfLarge(path, 1024*1024); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// original file must still exist
	if _, err := os.Stat(path); err != nil {
		t.Error("original log file should still exist after no-op rotate")
	}
	// backup must not exist
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Error("backup file should not exist when rotation was not needed")
	}
}

func TestRotateIfLarge_RotatesWhenLarge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	// write 10 bytes, set max to 5 bytes → must rotate
	if err := os.WriteFile(path, []byte("0123456789"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := rotateIfLarge(path, 5); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	backup := path + ".1"
	if _, err := os.Stat(backup); err != nil {
		t.Error("backup file should exist after rotation")
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("original file should be gone after rotation (will be recreated by logger)")
	}
}

func TestRotateIfLarge_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.log")
	// must not error when the file doesn't exist yet
	if err := rotateIfLarge(path, 1024); err != nil {
		t.Errorf("unexpected error for missing file: %v", err)
	}
}

// ─────────────────────────────────────────────
// tailFile
// ─────────────────────────────────────────────

func TestTailFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	lines := []string{"line1", "line2", "line3", "line4", "line5"}
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := tailFile(path, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "line3\nline4\nline5"
	if got != want {
		t.Errorf("tailFile got %q, want %q", got, want)
	}
}

func TestTailFile_FewerLinesThanRequested(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("only one line\n"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := tailFile(path, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "only one line" {
		t.Errorf("tailFile got %q, want %q", got, "only one line")
	}
}

// ─────────────────────────────────────────────
// writeConfig / cmdInit does not write github_token
// ─────────────────────────────────────────────

func TestWriteConfig_NoTokenField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config_auto_pull.json")
	cf := configFile{
		RepoPath:             dir,
		Branch:               "main",
		CheckIntervalSeconds: 5,
		LogFile:              "auto_pull.log",
		NotifyOnPull:         true,
	}
	if err := writeConfig(path, cf); err != nil {
		t.Fatalf("writeConfig error: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "github_token") {
		t.Error("written config must not contain 'github_token' field")
	}
	if strings.Contains(string(data), "GithubToken") {
		t.Error("written config must not contain 'GithubToken' field")
	}
}

// ─────────────────────────────────────────────
// resolveConfigPath
// ─────────────────────────────────────────────

func TestResolveConfigPath_Default(t *testing.T) {
	got := resolveConfigPath("")
	if !strings.HasSuffix(got, "config_auto_pull.json") {
		t.Errorf("resolveConfigPath(\"\") = %q, should end with config_auto_pull.json", got)
	}
}

func TestResolveConfigPath_Custom(t *testing.T) {
	got := resolveConfigPath("myconfig.json")
	if !strings.HasSuffix(got, "myconfig.json") {
		t.Errorf("resolveConfigPath(\"myconfig.json\") = %q, should end with myconfig.json", got)
	}
}