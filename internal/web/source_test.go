package web

import (
	"io/fs"
	"os"
	"testing"
)

// sourceFS is this package's directory on disk, for tests that scan sources.
func sourceFS(t *testing.T) fs.FS {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return os.DirFS(wd)
}
