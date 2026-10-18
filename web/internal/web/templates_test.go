package web

import (
	"path/filepath"
	"testing"
)

func TestTemplatesParse(t *testing.T) {
	if _, err := New(Options{
		AssetsDir:     "../../assets",
		AuthStorePath: filepath.Join(t.TempDir(), "auth"),
	}); err != nil {
		t.Fatal(err)
	}
}
