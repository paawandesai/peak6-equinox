package security

import (
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test directory to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above test directory")
		}
		dir = parent
	}
}

func git(t *testing.T, root string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // fixed binary; args are test literals
	cmd.Dir = root
	out, err := cmd.Output()
	return string(out), err
}

// TestAB06_GitignoreIgnoresEnv checks that local secret files are gitignored.
func TestAB06_GitignoreIgnoresEnv(t *testing.T) {
	root := repoRoot(t)
	for _, p := range []string{".env", ".env.local", "sub/dir/.env", "config.local", "bin/equinox", "coverage.out"} {
		_, err := git(t, root, "check-ignore", "-q", "--no-index", p)
		var exitErr *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exitErr) && exitErr.ExitCode() == 1:
			t.Errorf("%s is not ignored by .gitignore", p)
		default:
			t.Fatalf("git check-ignore %s: %v", p, err)
		}
	}
	if _, err := git(t, root, "check-ignore", "-q", "--no-index", ".env.example"); err == nil {
		t.Error(".env.example must stay tracked but is ignored")
	}
}

// TestAB06_NoTrackedEnvFiles checks that no env file other than .env.example is tracked.
func TestAB06_NoTrackedEnvFiles(t *testing.T) {
	root := repoRoot(t)
	out, err := git(t, root, "ls-files", "-z")
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	files := strings.Split(strings.TrimRight(out, "\x00"), "\x00")
	if len(files) == 0 || files[0] == "" {
		t.Fatal("git ls-files returned no files")
	}
	for _, f := range files {
		if isEnvFile(path.Base(f)) && f != ".env.example" {
			t.Errorf("tracked env file %q: secrets must not be committed", f)
		}
	}
}

func TestAB06_IsEnvFile(t *testing.T) {
	cases := map[string]bool{
		".env": true, ".env.local": true, ".env.prod": true, "prod.env": true,
		".env.example": true, "env.go": false, "environment.md": false, ".envrc": false,
	}
	for name, want := range cases {
		if got := isEnvFile(name); got != want {
			t.Errorf("isEnvFile(%q) = %v, want %v", name, got, want)
		}
	}
}

func isEnvFile(base string) bool {
	return base == ".env" || strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env")
}
