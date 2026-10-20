package web

import (
	"html"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// helpDocs — белый список документов: ключ → файл относительно HelpDir.
// Ключ используется в URL /help?doc=..., поэтому никаких путей из запроса
// напрямую не берём.
var helpDocs = map[string]string{
	"README":                "README.md",
	"start":                 "start.md",
	"admin":                 "admin.md",
	"pages/login":           "pages/login.md",
	"pages/info":            "pages/info.md",
	"pages/map":             "pages/map.md",
	"pages/map-settings":    "pages/map-settings.md",
	"pages/kes":             "pages/kes.md",
	"pages/kes-settings":    "pages/kes-settings.md",
	"pages/bms":             "pages/bms.md",
	"pages/bms-settings":    "pages/bms-settings.md",
	"pages/grid":            "pages/grid.md",
	"pages/grid-settings":   "pages/grid-settings.md",
	"pages/network":         "pages/network.md",
	"pages/system":          "pages/system.md",
	"pages/services":        "pages/services.md",
	"pages/logs":            "pages/logs.md",
	"pages/email":           "pages/email.md",
	"pages/events":          "pages/events.md",
	"pages/stats-charts":    "pages/stats-charts.md",
	"pages/stats-tables":    "pages/stats-tables.md",
	"pages/stats-settings":  "pages/stats-settings.md",
	"pages/master":          "pages/master.md",
	"pages/master-settings": "pages/master-settings.md",
	"api-legacy":            "api-legacy.md",
	"api-native":            "api-native.md",
}

// helpDocFor возвращает ключ документа для страницы. Пусто — справки нет.
func helpDocFor(urlPath string) string {
	switch urlPath {
	case "/":
		return "README"
	case "/login":
		return "start"
	case "/info":
		return "pages/info"
	case "/logs":
		return "pages/logs"
	case "/map":
		return "pages/map"
	case "/map/settings":
		return "pages/map-settings"
	case "/kes":
		return "pages/kes"
	case "/kes/settings":
		return "pages/kes-settings"
	case "/bms":
		return "pages/bms"
	case "/bms/settings":
		return "pages/bms-settings"
	case "/grid":
		return "pages/grid"
	case "/grid/settings":
		return "pages/grid-settings"
	case "/network":
		return "pages/network"
	case "/system":
		return "pages/system"
	case "/system/email":
		return "pages/email"
	case "/system/events":
		return "pages/events"
	case "/services":
		return "pages/services"
	case "/stats/charts":
		return "pages/stats-charts"
	case "/stats/tables":
		return "pages/stats-tables"
	case "/stats/settings":
		return "pages/stats-settings"
	case "/master":
		return "pages/master"
	case "/master/settings":
		return "pages/master-settings"
	}
	return ""
}

func (s *Server) handleHelp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	key := r.URL.Query().Get("doc")
	rel, ok := helpDocs[key]
	if !ok {
		http.NotFound(w, r)
		return
	}
	// До входа доступен только документ «С чего начать» (справка на странице
	// входа). Остальные документы — после авторизации.
	if _, authed := s.currentUser(r); !authed && key != "start" {
		http.NotFound(w, r)
		return
	}
	lang := s.lang(r)
	data, err := os.ReadFile(filepath.Join(s.helpDir, filepath.FromSlash(rel)))
	if lang != "ru" {
		if tr, err2 := os.ReadFile(filepath.Join(s.helpDir, "i18n", lang, filepath.FromSlash(rel))); err2 == nil {
			data, err = tr, nil
		}
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body := renderHelpMarkdown(string(data), key)
	title := "Помощь"
	if s.bundle != nil {
		if t := s.tr(lang, "help.title"); t != "" {
			title = t
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(helpPageHTML(body, lang, title)))
}

// handleHelpAsset отдаёт картинки из каталога manuals/img (если появятся).
func (s *Server) handleHelpAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if _, authed := s.currentUser(r); !authed {
		http.NotFound(w, r)
		return
	}
	p := path.Clean("/" + r.URL.Query().Get("path"))
	if !strings.HasPrefix(p, "/img/") {
		http.NotFound(w, r)
		return
	}
	full := filepath.Join(s.helpDir, filepath.FromSlash(strings.TrimPrefix(p, "/")))
	http.ServeFile(w, r, full)
}

func helpPageHTML(body, lang, title string) string {
	title = html.EscapeString(title)
	return `<!doctype html>
<html lang="` + html.EscapeString(lang) + `">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + title + `</title>
<style>
:root { color-scheme: light; }
* { box-sizing: border-box; }
body { margin: 0; padding: 28px 34px 48px; font: 16px/1.55 -apple-system, "Segoe UI", Roboto, Arial, sans-serif; color: #1d2b26; background: #f7f9f8; }
article { max-width: 980px; margin: 0 auto; }
h1 { font-size: 28px; margin: 0 0 18px; color: #0d5637; }
h2 { font-size: 22px; margin: 30px 0 10px; color: #0d5637; border-bottom: 1px solid #d7e0db; padding-bottom: 6px; }
h3 { font-size: 18px; margin: 22px 0 8px; }
p, ul, ol, table, pre, blockquote { margin: 0 0 14px; }
li { margin: 3px 0; }
ul, ol { padding-left: 24px; }
a { color: #136c47; }
code { background: #e8efeb; border-radius: 4px; padding: 1px 5px; font-size: 0.92em; }
pre { background: #10231b; color: #e8f2ec; padding: 14px 16px; border-radius: 8px; overflow: auto; }
pre code { background: none; padding: 0; color: inherit; }
blockquote { border-left: 4px solid #b9cdc2; padding: 2px 14px; color: #41544b; background: #eef3f0; }
table { border-collapse: collapse; width: 100%; }
th, td { border: 1px solid #cdd9d2; padding: 7px 9px; text-align: left; vertical-align: top; }
th { background: #e8efeb; }
hr { border: 0; border-top: 1px solid #cdd9d2; margin: 26px 0; }
img { max-width: 100%; height: auto; }
@media (max-width: 640px) { body { padding: 18px 14px 32px; } h1 { font-size: 23px; } table { font-size: 14px; } }
</style>
</head>
<body><article>` + body + `</article>
<script>
document.addEventListener("keydown", function (e) {
  if (e.key === "Escape" && window.parent !== window) {
    window.parent.postMessage({type: "malina-help-close"}, "*");
  }
});
if (location.hash) {
  var el = document.getElementById(decodeURIComponent(location.hash.slice(1)));
  if (el) el.scrollIntoView();
}
</script></body></html>`
}

var (
	reCode   = regexp.MustCompile("`([^`]+)`")
	reImage  = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]+)\)`)
	reLink   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	reBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	reItalic = regexp.MustCompile(`(^|[^*])\*([^*\n]+)\*`)
)

// renderHelpMarkdown — минимальный markdown-рендер под наши документы:
// заголовки, абзацы, списки, таблицы, цитаты, код, ссылки, картинки.
func renderHelpMarkdown(src, docKey string) string {
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	var b strings.Builder
	var para []string
	var list []string
	listOrdered := false
	inCode := false

	flushPara := func() {
		if len(para) == 0 {
			return
		}
		b.WriteString("<p>" + inlineHelp(strings.Join(para, " "), docKey) + "</p>\n")
		para = nil
	}
	flushList := func() {
		if len(list) == 0 {
			return
		}
		tag := "ul"
		if listOrdered {
			tag = "ol"
		}
		b.WriteString("<" + tag + ">\n")
		for _, it := range list {
			b.WriteString("<li>" + inlineHelp(it, docKey) + "</li>\n")
		}
		b.WriteString("</" + tag + ">\n")
		list = nil
	}
	flush := func() { flushPara(); flushList() }

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)

		if inCode {
			if strings.HasPrefix(trim, "```") {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				b.WriteString(html.EscapeString(line) + "\n")
			}
			continue
		}
		if strings.HasPrefix(trim, "```") {
			flush()
			b.WriteString("<pre><code>")
			inCode = true
			continue
		}
		if trim == "" {
			flush()
			continue
		}
		if trim == "---" || trim == "***" {
			flush()
			b.WriteString("<hr>\n")
			continue
		}
		if strings.HasPrefix(trim, "#") {
			n := 0
			for n < len(trim) && trim[n] == '#' {
				n++
			}
			if n <= 6 && n < len(trim) && trim[n] == ' ' {
				flush()
				tag := "h" + string('0'+byte(n))
				b.WriteString("<" + tag + ">" + inlineHelp(strings.TrimSpace(trim[n:]), docKey) + "</" + tag + ">\n")
				continue
			}
		}
		if strings.HasPrefix(trim, "|") {
			// таблица: header | --- | rows
			if i+1 < len(lines) && isTableSep(lines[i+1]) {
				flush()
				b.WriteString("<table>\n<thead><tr>")
				for _, c := range splitRow(trim) {
					b.WriteString("<th>" + inlineHelp(c, docKey) + "</th>")
				}
				b.WriteString("</tr></thead>\n<tbody>\n")
				i += 2
				for ; i < len(lines); i++ {
					row := strings.TrimSpace(lines[i])
					if !strings.HasPrefix(row, "|") {
						i--
						break
					}
					b.WriteString("<tr>")
					for _, c := range splitRow(row) {
						b.WriteString("<td>" + inlineHelp(c, docKey) + "</td>")
					}
					b.WriteString("</tr>\n")
				}
				b.WriteString("</tbody></table>\n")
				continue
			}
		}
		if strings.HasPrefix(trim, "> ") {
			flush()
			b.WriteString("<blockquote>" + inlineHelp(strings.TrimSpace(trim[2:]), docKey) + "</blockquote>\n")
			continue
		}
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") {
			flushPara()
			if listOrdered {
				flushList()
			}
			listOrdered = false
			list = append(list, strings.TrimSpace(trim[2:]))
			continue
		}
		if isOrderedItem(trim) {
			flushPara()
			if !listOrdered {
				flushList()
			}
			listOrdered = true
			list = append(list, strings.TrimSpace(trim[strings.Index(trim, ".")+1:]))
			continue
		}
		if len(list) > 0 {
			// продолжение пункта списка
			list[len(list)-1] += " " + trim
			continue
		}
		para = append(para, trim)
	}
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	flush()
	return b.String()
}

func isOrderedItem(s string) bool {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(s) && s[i] == '.' && s[i+1] == ' '
}

func isTableSep(s string) bool {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "|") {
		return false
	}
	for _, c := range splitRow(s) {
		c = strings.TrimSpace(c)
		c = strings.Trim(c, "-:")
		if c != "" {
			return false
		}
	}
	return true
}

func splitRow(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

// inlineHelp экранирует HTML и размечает инлайн: код, картинки, ссылки, жирный,
// курсив. Ссылки на .md превращаются в переходы внутри справки.
func inlineHelp(s, docKey string) string {
	out := html.EscapeString(s)

	// код — до ссылок, чтобы не размечать содержимое.
	out = reCode.ReplaceAllString(out, "<code>$1</code>")

	out = reImage.ReplaceAllStringFunc(out, func(m string) string {
		sub := reImage.FindStringSubmatch(m)
		alt, src := sub[1], sub[2]
		return `<img src="` + helpImageURL(docKey, src) + `" alt="` + alt + `">`
	})

	out = reLink.ReplaceAllStringFunc(out, func(m string) string {
		sub := reLink.FindStringSubmatch(m)
		text, target := sub[1], sub[2]
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			return `<a href="` + target + `" target="_blank" rel="noopener">` + text + `</a>`
		}
		key, ok := resolveHelpDoc(docKey, target)
		if !ok {
			return text
		}
		return `<a href="/help?doc=` + key + `">` + text + `</a>`
	})

	out = reBold.ReplaceAllString(out, "<strong>$1</strong>")
	out = reItalic.ReplaceAllString(out, "$1<em>$2</em>")
	return out
}

func helpAssetURL(src string) string {
	src = strings.TrimPrefix(src, "./")
	if strings.HasPrefix(src, "/") {
		src = strings.TrimPrefix(src, "/")
	}
	return "/help-asset?path=" + src
}

// helpImageURL переводит путь картинки относительно документа в путь manuals/.
// Пример: pages/info.md + ../img/pages/info.png → img/pages/info.png.
func helpImageURL(docKey, src string) string {
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		return src
	}
	if strings.HasPrefix(src, "/") {
		src = strings.TrimPrefix(src, "/")
	} else {
		src = path.Clean(path.Join(path.Dir(helpDocs[docKey]), src))
	}
	return helpAssetURL(src)
}

// resolveHelpDoc переводит относительную ссылку .md в ключ справки.
func resolveHelpDoc(fromKey, target string) (string, bool) {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		target = target[:i]
	}
	if !strings.HasSuffix(target, ".md") {
		return "", false
	}
	var rel string
	if strings.HasPrefix(target, "/") {
		rel = strings.TrimPrefix(target, "/")
	} else {
		dir := path.Dir(helpDocs[fromKey])
		rel = path.Clean(path.Join(dir, target))
	}
	for key, file := range helpDocs {
		if file == rel {
			return key, true
		}
	}
	// Короткие ссылки вида services.md → pages/services.md.
	for key, file := range helpDocs {
		if file == path.Join("pages", rel) {
			return key, true
		}
	}
	return "", false
}
