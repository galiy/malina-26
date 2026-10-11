package masternode

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDefaultsAndDup(t *testing.T) {
	c := Config{Nodes: []Node{
		{Name: "A", Host: "10.0.0.1"}, // scheme/port по умолчанию
		{Name: "B", Host: "node.local", Port: 80},
	}}
	if err := Validate(&c); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if c.Nodes[0].Scheme != "http" || c.Nodes[0].Port != 80 {
		t.Fatalf("дефолты не проставлены: %+v", c.Nodes[0])
	}
	if len(c.Nodes[0].ID) != 8 {
		t.Fatalf("id не сгенерирован: %q", c.Nodes[0].ID)
	}
	// дубль host:port
	c.Nodes = append(c.Nodes, Node{Name: "C", Host: "10.0.0.1", Port: 80})
	if err := Validate(&c); err == nil {
		t.Fatal("дубль host:port должен отвергаться")
	}
}

func TestValidateBadFields(t *testing.T) {
	cases := []Config{
		{Nodes: []Node{{Name: "", Host: "1.2.3.4"}}},                      // пустое имя
		{Nodes: []Node{{Name: strings.Repeat("x", 33), Host: "1.2.3.4"}}}, // имя > 32
		{Nodes: []Node{{Name: "A", Host: "1.2.3.4", Scheme: "ftp"}}},      // bad scheme
		{Nodes: []Node{{Name: "A", Host: "bad host"}}},                    // bad host
		{Nodes: []Node{{Name: "A", Host: "1.2.3.4", Port: 70000}}},        // bad port
	}
	for i := range cases {
		if err := Validate(&cases[i]); err == nil {
			t.Fatalf("кейс %d: ожидали ошибку", i)
		}
	}
}

func TestValidateBadID(t *testing.T) {
	c := Config{Nodes: []Node{{ID: "../../pwn", Name: "X", Host: "1.2.3.4"}}}
	if err := Validate(&c); err == nil {
		t.Fatal("некорректный id должен отвергаться")
	}
	if ValidID("../../pwn") || ValidID("AABBCCDD") || !ValidID("aabbccdd") {
		t.Fatal("ValidID работает неверно")
	}
}

func TestLimit(t *testing.T) {
	c := Config{}
	for i := 0; i < MaxNodes+1; i++ {
		c.Nodes = append(c.Nodes, Node{Name: "n", Host: "10.0.0.1", Port: 1000 + i})
	}
	if err := Validate(&c); err == nil {
		t.Fatal("превышение лимита должно отвергаться")
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "master", "nodes.json")
	in := Config{Nodes: []Node{{Name: "Garage", Scheme: "https", Host: "node1.local", Port: 8443}}}
	if err := Save(p, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.Version != 1 || len(out.Nodes) != 1 || out.Nodes[0].Name != "Garage" || out.Nodes[0].Scheme != "https" {
		t.Fatalf("roundtrip: %+v", out)
	}
	if out.Nodes[0].ID == "" {
		t.Fatal("id должен сохраняться/генерироваться")
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || len(c.Nodes) != 0 || c.Version != 1 {
		t.Fatalf("отсутствие файла → пустой конфиг: %+v, %v", c, err)
	}
}
