package store_test

import "testing"

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
