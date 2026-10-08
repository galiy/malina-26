package i18n

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
)

// Bundle хранит словари переводов по языкам.
type Bundle struct {
	langs map[string]map[string]string
	def   string
}

// Load читает <dir>/<lang>.json для каждого языка из fsys.
func Load(fsys fs.FS, dir, def string, langs []string) (*Bundle, error) {
	b := &Bundle{langs: make(map[string]map[string]string), def: def}
	for _, l := range langs {
		data, err := fs.ReadFile(fsys, path.Join(dir, l+".json"))
		if err != nil {
			return nil, fmt.Errorf("i18n %s: %w", l, err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("i18n %s: %w", l, err)
		}
		b.langs[l] = m
	}
	if _, ok := b.langs[def]; !ok {
		return nil, fmt.Errorf("i18n: default language %q missing", def)
	}
	return b, nil
}

// Has сообщает, доступен ли язык.
func (b *Bundle) Has(lang string) bool {
	_, ok := b.langs[lang]
	return ok
}

// Translations возвращает строки базового языка, перекрытые выбранным.
func (b *Bundle) Translations(lang string) map[string]string {
	out := make(map[string]string, len(b.langs[b.def]))
	for k, v := range b.langs[b.def] {
		out[k] = v
	}
	if m, ok := b.langs[lang]; ok {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}
