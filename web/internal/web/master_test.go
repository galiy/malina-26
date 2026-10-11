package web

import (
	"testing"

	"github.com/galiy/malina-26/web/internal/masternode"
)

func TestNodesOrDefaults(t *testing.T) {
	got := nodesOrDefaults(nil)
	if len(got) != 1 {
		t.Fatalf("ожидали 1 узел по умолчанию, got=%d", len(got))
	}
	n := got[0]
	if n.Name != "Локальный" || n.Scheme != "http" || n.Host != "localhost" || n.Port != 80 {
		t.Fatalf("дефолтный узел неверный: %+v", n)
	}
	// непустой список не трогаем
	in := []masternode.Node{{Name: "X", Host: "1.2.3.4"}}
	if out := nodesOrDefaults(in); len(out) != 1 || out[0].Name != "X" {
		t.Fatalf("непустой список изменён: %+v", out)
	}
}
