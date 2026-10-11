package web

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// TestLegacyContractVersionMatchesReader страхует от дрейфа версии бинарного
// контракта. legacyContractVersion (этот пакет) обязан совпадать с
// contractVersion в daemons/map-reader/contract.go.
//
// Расхождение молча ломает внешнего легаси-потребителя — агрегатор
// «Мастер-Узел» (master_node.php на других узлах): decodeLegacy отвергает
// payload, снимок read_json.php не обновляется, check_daemons.php отдаёт
// mapd=1, и телеметрия МАП перестаёт приходить на страницу «Отобразить»
// (см. docs/legacy/master-node.md). Локальный UI этого не замечает, т.к.
// assets/static/map.js версию не сверяет.
func TestLegacyContractVersionMatchesReader(t *testing.T) {
	path := filepath.Join("..", "..", "..", "daemons", "map-reader", "contract.go")
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("не удалось разобрать %s: %v", path, err)
	}

	found := false
	readerVer := 0
	ast.Inspect(f, func(n ast.Node) bool {
		gd, ok := n.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			return true
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if name.Name != "contractVersion" || i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.INT {
					continue
				}
				v, err := strconv.Atoi(lit.Value)
				if err != nil {
					continue
				}
				readerVer, found = v, true
			}
		}
		return true
	})
	if !found {
		t.Fatalf("в %s не найдена константа contractVersion", path)
	}
	if legacyContractVersion != readerVer {
		t.Fatalf("дрейф версии контракта: legacyContractVersion=%d, map-reader contractVersion=%d; "+
			"правьте обе константы синхронно (docs/map-contract.md, «Зависимости контракта»)",
			legacyContractVersion, readerVer)
	}
}
