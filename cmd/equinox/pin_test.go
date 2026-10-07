package main

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func TestPinCommand(t *testing.T) {
	dbEnv(t)
	tdb.Reset(t)
	ctx := context.Background()
	st, err := store.Open(ctx, tdb.DSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	done, err := st.CreateRound(ctx, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRound(ctx, done.ID, store.RoundOK, nil, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	running, err := st.CreateRound(ctx, t0.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if code := run(ctx, []string{"pin", strconv.FormatInt(done.ID, 10)}, &out, &errOut); code != 0 {
		t.Fatalf("pin: exit %d, stderr %q", code, errOut.String())
	}
	if !strings.Contains(out.String(), "pinned round "+strconv.FormatInt(done.ID, 10)) {
		t.Errorf("stdout %q", out.String())
	}
	cur, err := st.CurrentRound(ctx)
	if err != nil || cur.ID != done.ID || !cur.Pinned {
		t.Errorf("CurrentRound = %+v, %v; want pinned round %d", cur, err, done.ID)
	}

	errOut.Reset()
	if code := run(ctx, []string{"pin", strconv.FormatInt(running.ID, 10)}, io.Discard, &errOut); code != 1 ||
		!strings.Contains(errOut.String(), "not found or not finished") {
		t.Errorf("pin running round: exit %d, stderr %q", code, errOut.String())
	}
	if cur, _ := st.CurrentRound(ctx); cur.ID != done.ID {
		t.Errorf("a failed pin changed the current round to %d", cur.ID)
	}

	out.Reset()
	if code := run(ctx, []string{"pin", "--clear"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "unpinned") {
		t.Errorf("pin --clear: exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
	if _, err := st.PinnedRound(ctx); err == nil {
		t.Error("a round is still pinned after --clear")
	}

	for _, args := range [][]string{{"pin"}, {"pin", "abc"}, {"pin", "0"}, {"pin", "1", "2"}, {"pin", "--clear", "1"}} {
		if code := run(ctx, args, io.Discard, io.Discard); code != 2 {
			t.Errorf("%v: exit %d, want 2 (usage)", args, code)
		}
	}
}
