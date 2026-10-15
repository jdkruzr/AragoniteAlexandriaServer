package web

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

var (
	tagOrScript = regexp.MustCompile(`(?s)<script.*?</script>|<style.*?</style>|<code>.*?</code>|<[^>]+>`)
	englishWord = regexp.MustCompile(`(?i)\b(the|and|your|with|this|from|pages|notebook|settings)\b`)
)

// Every preview renders in Spanish through the same per-request functions as
// live pages. Visible text that still reads as English is logged, to find
// strings that bypass the catalog.
func TestPreviewPagesRenderInSpanish(t *testing.T) {
	es := i18n.For("es")
	if es.Lang() != "es" {
		t.Fatalf("Spanish catalog not loaded: %s", es.Lang())
	}
	out := os.Getenv("PREVIEW_ES_OUT")
	for _, p := range previewPages() {
		clone, err := layout.Clone()
		if err == nil {
			_, err = clone.ParseFS(assets, "templates/"+p.template+".html")
		}
		var buf bytes.Buffer
		if err == nil {
			clone.Funcs(template.FuncMap{"t": es.T, "th": htmlT(es), "td": detailIn(es), "tip": tipIn(es)})
			err = clone.ExecuteTemplate(&buf, "layout.html", page{Lang: es.Lang(), Title: pageTitle(es, p.title), Section: p.section, SourceTab: sourceTab(p.path, p.section),
				ClientSource: p.section == "notebooks" || p.section == "books" || p.section == "devices" || p.section == "client-settings",
				NativeBOOX:   true, Base: "https://library.example", Data: p.data})
		}
		if err != nil {
			t.Fatalf("%s: %v", p.file, err)
		}
		if !bytes.Contains(buf.Bytes(), []byte(`<html lang="es">`)) {
			t.Errorf("%s: missing lang=es", p.file)
		}
		for _, line := range strings.Split(tagOrScript.ReplaceAllString(buf.String(), "\n"), "\n") {
			if line = strings.TrimSpace(line); englishWord.MatchString(line) {
				t.Logf("%s: still English? %q", p.file, line)
			}
		}
		if out != "" {
			_ = os.WriteFile(filepath.Join(out, p.file), buf.Bytes(), 0o644)
		}
	}
}
