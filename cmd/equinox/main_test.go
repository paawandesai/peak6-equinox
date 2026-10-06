package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/config"
)

func TestRunPlannedSubcommandsExitZero(t *testing.T) {
	for name, ticket := range planned {
		if _, registered := commands[name]; registered {
			continue
		}
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{name}, &out, &errOut); code != 0 {
			t.Errorf("%s: exit %d, stderr %q", name, code, errOut.String())
		}
		want := "not implemented yet (ticket " + ticket + ")"
		if !strings.Contains(out.String(), want) {
			t.Errorf("%s: stdout %q does not contain %q", name, out.String(), want)
		}
	}
}

func TestRunUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"nope"}, &out, &errOut); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `unknown subcommand "nope"`) || !strings.Contains(errOut.String(), "serve") {
		t.Errorf("stderr = %q, want error and usage", errOut.String())
	}
}

func TestRunHelpListsCommands(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"help"}, &out, io.Discard); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	for _, n := range []string{"serve", "version"} {
		if !strings.Contains(out.String(), "  "+n+"\n") {
			t.Errorf("help %q does not list %s", out.String(), n)
		}
	}
}

func TestRunVersion(t *testing.T) {
	var out bytes.Buffer
	if code := run(context.Background(), []string{"version"}, &out, io.Discard); code != 0 {
		t.Fatalf("exit = %d", code)
	}
	if !strings.HasPrefix(out.String(), "equinox dev") {
		t.Errorf("version output = %q", out.String())
	}
}

func TestBuildVersionUsesLinkerValue(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "v1.2.3"
	if got := buildVersion(); got != "v1.2.3" {
		t.Errorf("buildVersion() = %q", got)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("duplicate register did not panic")
		}
	}()
	register("serve", runServe)
}

func TestServeFailsFastWithoutRequiredEnv(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("DATABASE_URL", "")
	var errOut bytes.Buffer
	if code := run(context.Background(), []string{"serve"}, io.Discard, &errOut); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "APP_ENV is required") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

// TestServeHealthzWithoutDB starts the real server with a DATABASE_URL that
// points nowhere and checks /healthz still answers 200.
func TestServeHealthzWithoutDB(t *testing.T) {
	cfg, err := config.Load(func(k string) (string, bool) {
		v, ok := map[string]string{
			"APP_ENV":        "ci",
			"DATABASE_URL":   "postgres://nobody@127.0.0.1:1/none",
			"ADMIN_USER":     "admin",
			"ADMIN_PASSWORD": "test-only-password-0123456789",
		}[k]
		return v, ok
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	logger := slog.New(slog.DiscardHandler)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, newServer(cfg, logger), logger) }()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != `{"status":"ok"}` {
		t.Errorf("GET /healthz = %d %q", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned %v after shutdown", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("serve did not shut down")
	}
}

func TestUTCTime(t *testing.T) {
	local := time.Date(2026, 10, 6, 13, 0, 0, 0, time.FixedZone("CDT", -5*3600))
	got := utcTime(nil, slog.Time(slog.TimeKey, local))
	if got.Value.Time().Location() != time.UTC || !got.Value.Time().Equal(local) {
		t.Errorf("utcTime = %v, want %v in UTC", got.Value.Time(), local)
	}
	other := slog.Time("fetched_at", local)
	if utcTime([]string{"g"}, other).Value.Time().Location() == time.UTC {
		t.Error("utcTime changed a non-top-level attr")
	}
}
