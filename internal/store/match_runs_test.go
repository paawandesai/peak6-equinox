package store_test

import (
	"errors"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

func TestInsertMatchRun(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)
	if err := s.FinishRound(ctx, r.ID, store.RoundOK, nil, t0); err != nil {
		t.Fatal(err)
	}

	m := store.MatchRun{
		RoundID:             r.ID,
		StartedAt:           t0,
		FinishedAt:          t0.Add(5 * time.Second),
		DurationMS:          5000,
		PairsCompared:       100,
		RelationshipsStored: 50,
		Outcome:             store.MatchRunOK,
	}
	id, err := s.InsertMatchRun(ctx, m)
	if err != nil {
		t.Fatalf("InsertMatchRun: %v", err)
	}
	if id == 0 {
		t.Error("id = 0")
	}

	got, err := s.GetLatestMatchRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestMatchRun: %v", err)
	}
	if got.ID != id {
		t.Errorf("got.ID = %d, want %d", got.ID, id)
	}
	if got.RoundID != r.ID {
		t.Errorf("got.RoundID = %d, want %d", got.RoundID, r.ID)
	}
	if got.DurationMS != 5000 {
		t.Errorf("got.DurationMS = %d, want 5000", got.DurationMS)
	}
	if got.PairsCompared != 100 {
		t.Errorf("got.PairsCompared = %d, want 100", got.PairsCompared)
	}
	if got.RelationshipsStored != 50 {
		t.Errorf("got.RelationshipsStored = %d, want 50", got.RelationshipsStored)
	}
	if got.Outcome != store.MatchRunOK {
		t.Errorf("got.Outcome = %q, want %q", got.Outcome, store.MatchRunOK)
	}
}

func TestInsertMatchRunFailed(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)
	if err := s.FinishRound(ctx, r.ID, store.RoundOK, nil, t0); err != nil {
		t.Fatal(err)
	}

	m := store.MatchRun{
		RoundID:             r.ID,
		StartedAt:           t0,
		FinishedAt:          t0.Add(2 * time.Second),
		DurationMS:          2000,
		PairsCompared:       10,
		RelationshipsStored: 0,
		Outcome:             store.MatchRunFailed,
	}
	id, err := s.InsertMatchRun(ctx, m)
	if err != nil {
		t.Fatalf("InsertMatchRun: %v", err)
	}

	got, err := s.GetLatestMatchRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestMatchRun: %v", err)
	}
	if got.ID != id {
		t.Errorf("got.ID = %d, want %d", got.ID, id)
	}
	if got.Outcome != store.MatchRunFailed {
		t.Errorf("got.Outcome = %q, want %q", got.Outcome, store.MatchRunFailed)
	}
}

func TestMatchRunRetention(t *testing.T) {
	s := newStore(t)
	r := mustRound(t, s)
	if err := s.FinishRound(ctx, r.ID, store.RoundOK, nil, t0); err != nil {
		t.Fatal(err)
	}

	// Insert 505 match runs. The retention cap is 500, so the oldest 5 should be deleted.
	const count = 505
	ids := make([]int64, count)
	for i := 0; i < count; i++ {
		m := store.MatchRun{
			RoundID:             r.ID,
			StartedAt:           t0.Add(time.Duration(i) * time.Minute),
			FinishedAt:          t0.Add(time.Duration(i)*time.Minute + time.Second),
			DurationMS:          1000,
			PairsCompared:       10,
			RelationshipsStored: 5,
			Outcome:             store.MatchRunOK,
		}
		id, err := s.InsertMatchRun(ctx, m)
		if err != nil {
			t.Fatalf("InsertMatchRun[%d]: %v", i, err)
		}
		ids[i] = id
	}

	// The latest run should be the last one inserted.
	latest, err := s.GetLatestMatchRun(ctx)
	if err != nil {
		t.Fatalf("GetLatestMatchRun: %v", err)
	}
	if latest.ID != ids[count-1] {
		t.Errorf("latest.ID = %d, want %d", latest.ID, ids[count-1])
	}

	// ListMatchRuns with limit 100 should return at most 100 runs.
	runs, err := s.ListMatchRuns(ctx, 100)
	if err != nil {
		t.Fatalf("ListMatchRuns: %v", err)
	}
	if len(runs) != 100 {
		t.Errorf("len(runs) = %d, want 100", len(runs))
	}

	// ListMatchRuns with limit 1000 should return at most 500 runs (the retention cap).
	runs, err = s.ListMatchRuns(ctx, 1000)
	if err != nil {
		t.Fatalf("ListMatchRuns: %v", err)
	}
	if len(runs) != 500 {
		t.Errorf("len(runs) = %d, want 500 (retention cap)", len(runs))
	}

	// The oldest run should be ids[5], not ids[0..4].
	if runs[len(runs)-1].ID != ids[5] {
		t.Errorf("oldest run ID = %d, want %d (ids[5])", runs[len(runs)-1].ID, ids[5])
	}

	// The newest run should be ids[504].
	if runs[0].ID != ids[count-1] {
		t.Errorf("newest run ID = %d, want %d (ids[504])", runs[0].ID, ids[count-1])
	}
}

func TestGetLatestMatchRunNotFound(t *testing.T) {
	s := newStore(t)
	_, err := s.GetLatestMatchRun(ctx)
	if err == nil {
		t.Error("GetLatestMatchRun succeeded on empty table")
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestListMatchRunsEmpty(t *testing.T) {
	s := newStore(t)
	runs, err := s.ListMatchRuns(ctx, 10)
	if err != nil {
		t.Fatalf("ListMatchRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0", len(runs))
	}
}
