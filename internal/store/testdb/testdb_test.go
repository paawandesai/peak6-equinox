package testdb

import (
	"strings"
	"testing"
)

func TestWithSearchPath(t *testing.T) {
	got, err := withSearchPath("postgres://u:p@localhost:5432/db?sslmode=disable", "test_x_1")
	if err != nil || !strings.Contains(got, "search_path=test_x_1") || !strings.Contains(got, "sslmode=disable") {
		t.Errorf("url form = %q %v", got, err)
	}
	got, err = withSearchPath("host=localhost dbname=db", "test_x_1")
	if err != nil || got != "host=localhost dbname=db search_path=test_x_1" {
		t.Errorf("keyword form = %q %v", got, err)
	}
	if _, err := withSearchPath("postgres://bad\x7f:url", "s"); err == nil {
		t.Error("invalid URL accepted")
	}
}

func TestPackageName(t *testing.T) {
	if got := packageName(); got != "testdb" {
		t.Errorf("packageName() = %q, want testdb", got)
	}
}

func TestRequireWithoutRun(t *testing.T) {
	t.Setenv("CI", "")
	var d DB
	ok := t.Run("skips", func(t *testing.T) { d.Pool(t) })
	if !ok || !strings.Contains(d.reason, "Run was not called") {
		t.Errorf("reason = %q", d.reason)
	}
}
