package web

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Message IDs are English source text. These patterns find every place a
// message enters the UI, so catalogs can be checked against real use.
var (
	quoted = `"(?:\\.|[^"\\])*"`
	// {{t "ID" args…}}: plural when the only argument is a count or a "Count" pair is given.
	templateT  = regexp.MustCompile(`\{\{-?\s*th? (` + quoted + `)((?:` + quoted + `|[^"}]|\}[^}])*)\}\}`)
	goPatterns = []*regexp.Regexp{
		regexp.MustCompile(`i18n\.M\((` + quoted + `)\)`),
		regexp.MustCompile(`\b(?:render|renderPage)\(w, r, [^,]+, (` + quoted + `)`),
		regexp.MustCompile(`(?m)^\s*notice\w* *:?= *(` + quoted + `)`),
		regexp.MustCompile(`notice="\+url\.QueryEscape\((` + quoted + `)\)`),
	}
	// T("ID", args…) in Go: plural when the only argument is a count or a "Count" pair is given.
	goT        = regexp.MustCompile(`\bT\((?:r, )?(` + quoted + `)(?:, ([^)]*))?\)`)
	backNotice = regexp.MustCompile(`\bback\(w, r, [^,]+, (` + quoted + `)(, [^)]+)?\)`)
)

type usedMessages map[string]bool // ID → plural

func (u usedMessages) add(raw string, plural bool) {
	id, err := strconv.Unquote(raw)
	if err != nil || strings.TrimSpace(id) == "" {
		return
	}
	u[id] = u[id] || plural
}

func collectMessages(t *testing.T) usedMessages {
	t.Helper()
	used := usedMessages{}
	scanTemplate := func(src string) {
		for _, m := range templateT.FindAllStringSubmatch(src, -1) {
			args := strings.TrimSpace(m[2])
			plural := strings.Contains(args, `"Count"`) || (args != "" && !strings.HasPrefix(args, `"`) && len(splitArgs(args)) == 1)
			used.add(m[1], plural)
		}
	}
	_ = fs.WalkDir(assets, "templates", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := fs.ReadFile(assets, path)
			scanTemplate(string(b))
		}
		return nil
	})
	booxViews, _ := filepath.Glob("../../boox/templates/*")
	for _, f := range booxViews {
		b, _ := os.ReadFile(f)
		scanTemplate(string(b))
	}
	_ = filepath.WalkDir("../..", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(path)
		src := string(b)
		for _, p := range goPatterns {
			for _, m := range p.FindAllStringSubmatch(src, -1) {
				used.add(m[1], false)
			}
		}
		for _, m := range goT.FindAllStringSubmatch(src, -1) {
			args := strings.TrimSpace(m[2])
			used.add(m[1], strings.Contains(args, `"Count"`) || (args != "" && !strings.HasPrefix(args, `"`) && len(splitArgs(args)) == 1))
		}
		for _, m := range backNotice.FindAllStringSubmatch(src, -1) {
			used.add(m[1], m[2] != "")
		}
		return nil
	})
	for _, tip := range helpTips {
		used[tip.Title] = used[tip.Title]
		used[tip.Body] = used[tip.Body]
	}
	for _, id := range dynamicMessages {
		used[id] = used[id]
	}
	return used
}

// splitArgs splits template arguments on spaces outside quotes and parentheses.
func splitArgs(s string) []string {
	var out []string
	depth, quote, start := 0, false, 0
	for i, c := range s {
		switch {
		case c == '"' && (i == 0 || s[i-1] != '\\'):
			quote = !quote
		case quote:
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ' ' && depth == 0:
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

const localeDir = "../i18n/locales"

var (
	htmlTags    = regexp.MustCompile(`<[^>]*>`)
	placeholder = regexp.MustCompile(`\{\{\.\w+\}\}`)
)

// forms_ lists a catalog value's text: one string, or each plural form.
func forms_(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case map[string]any:
		var out []string
		for _, f := range x {
			if s, ok := f.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameSet compares placeholder names, ignoring order and repeats (a singular
// form may drop {{.Count}}, e.g. "one page").
func sameSet(a, b []string) bool {
	in := func(xs []string) map[string]bool {
		m := map[string]bool{}
		for _, x := range xs {
			m[x] = true
		}
		return m
	}
	ma, mb := in(a), in(b)
	for k := range ma {
		if !mb[k] {
			return false
		}
	}
	return true
}

func readCatalog(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c := map[string]any{}
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return c
}

// Every language must translate every message the UI uses, with plural forms
// where a count chooses the wording, and must not keep entries nothing uses.
// Rewording English therefore fails here until each language follows.
// I18N_UPDATE=1 rewrites the English source catalog from current use.
func TestCatalogsCoverEveryMessage(t *testing.T) {
	used := collectMessages(t)
	if os.Getenv("I18N_UPDATE") != "" {
		writeEnglish(t, used)
	}
	files, _ := filepath.Glob(filepath.Join(localeDir, "active.*.json"))
	if len(files) < 2 {
		t.Fatalf("expected English and at least one translation, found %v", files)
	}
	for _, f := range files {
		lang := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), "active."), ".json")
		cat := readCatalog(t, f)
		var missing, stale, forms []string
		for id, plural := range used {
			v, ok := cat[id]
			if !ok {
				missing = append(missing, id)
				continue
			}
			if obj, isObj := v.(map[string]any); plural && (!isObj || obj["one"] == nil || obj["other"] == nil) {
				forms = append(forms, id)
			}
		}
		for id := range cat {
			if _, ok := used[id]; !ok {
				stale = append(stale, id)
			}
		}
		report := func(kind string, ids []string) {
			sort.Strings(ids)
			for _, id := range ids {
				t.Errorf("%s: %s: %q", lang, kind, id)
			}
		}
		// Translators change words, never markup: every form must carry exactly
		// the English message's tags (attributes included) and placeholders.
		var markup []string
		for id := range used {
			for _, form := range forms_(cat[id]) {
				if !sameStrings(htmlTags.FindAllString(form, -1), htmlTags.FindAllString(id, -1)) || !sameSet(placeholder.FindAllString(form, -1), placeholder.FindAllString(id, -1)) {
					markup = append(markup, id)
					break
				}
			}
		}
		report("markup or placeholders differ from English", markup)
		report("missing", missing)
		report("needs one/other plural forms", forms)
		report("no longer used (reworded or removed?)", stale)
	}
}

func writeEnglish(t *testing.T, used usedMessages) {
	t.Helper()
	path := filepath.Join(localeDir, "active.en.json")
	old := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &old)
	}
	out := map[string]any{}
	for id, plural := range used {
		switch {
		case !plural:
			out[id] = id
		case old[id] != nil:
			out[id] = old[id] // keep the hand-written singular
		default:
			out[id] = map[string]string{"one": id, "other": id}
		}
	}
	if err := os.WriteFile(path, catalogJSON(out), 0o644); err != nil {
		t.Fatal(err)
	}
}

// catalogJSON keeps markup readable for translators (no \u003c escapes).
func catalogJSON(v any) []byte {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return []byte(b.String())
}
