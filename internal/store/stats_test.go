package store_test

import (
	"context"
	"testing"
)

func TestDatabaseSize(t *testing.T) {
	s := newStore(t)
	size, err := s.DatabaseSize(ctx)
	if err != nil {
		t.Fatalf("DatabaseSize: %v", err)
	}
	if size <= 0 {
		t.Errorf("DatabaseSize = %d, want > 0", size)
	}
}

func TestTryLockIngest(t *testing.T) {
	s := newStore(t)
	release, ok, err := s.TryLockIngest(ctx)
	if err != nil || !ok {
		t.Fatalf("first lock: ok=%v err=%v", ok, err)
	}
	// A second holder (another connection, as another instance would use) is refused.
	release2, ok2, err := s.TryLockIngest(ctx)
	if err != nil || ok2 {
		t.Fatalf("second lock while held: ok=%v err=%v", ok2, err)
	}
	if release2 != nil {
		t.Error("release returned for a lock that was not acquired")
	}
	release()
	release3, ok3, err := s.TryLockIngest(ctx)
	if err != nil || !ok3 {
		t.Fatalf("lock after release: ok=%v err=%v", ok3, err)
	}
	release3()
	dead, cancel := context.WithCancel(ctx)
	cancel()
	if _, ok, err := s.TryLockIngest(dead); err == nil || ok {
		t.Errorf("lock with a dead context: ok=%v err=%v", ok, err)
	}
}
