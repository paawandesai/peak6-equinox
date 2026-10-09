package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }

// setAppEnv sets the variables config.FromEnv requires.
func setAppEnv(t *testing.T, dbURL string) {
	t.Helper()
	t.Setenv("APP_ENV", "ci")
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("ADMIN_USER", "admin")
	t.Setenv("ADMIN_PASSWORD", "test-only-password-0123456789")
}

func TestMigrateCommandUpDown(t *testing.T) {
	setAppEnv(t, tdb.FreshSchema(t))
	for _, step := range []struct {
		args []string
		want string
	}{
		{[]string{"migrate"}, "0001_init.sql"}, // up applies all migrations
		{[]string{"migrate", "status"}, "applied"},
		{[]string{"migrate", "down"}, "down 0004"},           // down rolls back the latest (0004)
		{[]string{"migrate", "up"}, "up 0004"},               // up re-applies 0004
		{[]string{"migrate", "reset"}, "down 0001_init.sql"}, // reset rolls back all, ending with 0001
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), step.args, &out, &errOut); code != 0 {
			t.Fatalf("%v: exit %d, stderr %q", step.args, code, errOut.String())
		}
		if !strings.Contains(out.String(), step.want) {
			t.Errorf("%v: stdout %q, want %q", step.args, out.String(), step.want)
		}
	}
}

func TestMigrateCommandErrors(t *testing.T) {
	setAppEnv(t, "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	cases := []struct {
		args     []string
		code     int
		inStderr string
	}{
		{[]string{"migrate", "sideways"}, 2, "unknown migrate action"},
		{[]string{"migrate", "up", "extra"}, 2, "at most one action"},
		{[]string{"migrate", "up"}, 1, "migrate"},
	}
	for _, tc := range cases {
		var errOut bytes.Buffer
		if code := run(context.Background(), tc.args, io.Discard, &errOut); code != tc.code {
			t.Errorf("%v: exit %d, want %d (stderr %q)", tc.args, code, tc.code, errOut.String())
		}
		if !strings.Contains(errOut.String(), tc.inStderr) {
			t.Errorf("%v: stderr %q, want %q", tc.args, errOut.String(), tc.inStderr)
		}
	}
	t.Setenv("APP_ENV", "prod")
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"migrate", "reset"}, io.Discard, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "refused when APP_ENV=prod") {
		t.Errorf("reset in prod: exit %d, stderr %q", code, errOut.String())
	}
	t.Setenv("APP_ENV", "")
	errOut.Reset()
	if code := run(context.Background(), []string{"migrate"}, io.Discard, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "APP_ENV is required") {
		t.Errorf("missing env: exit %d, stderr %q", code, errOut.String())
	}
}
