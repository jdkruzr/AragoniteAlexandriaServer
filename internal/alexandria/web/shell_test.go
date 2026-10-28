package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

const shellOrigin = "https://alexandria.test"

func shellGet(t *testing.T, d Deps, path string, cookies ...*http.Cookie) string {
	t.Helper()
	r := httptest.NewRequest("GET", shellOrigin+path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	Handler(d).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body)
	}
	return w.Body.String()
}

func TestShellHeaderAndTheme(t *testing.T) {
	d := Deps{PublicURL: shellOrigin}
	page := shellGet(t, d, "/help")
	for _, want := range []string{`<html lang="en">`, `action="/search"`, `name="theme" value="dark"`, `name="theme" value="light"`, `name="back" value="/help"`, `class="icon"`} {
		if !strings.Contains(page, want) {
			t.Errorf("header lacks %q", want)
		}
	}
	if strings.Contains(page, "source-nav") || strings.Contains(page, "source-switch") || strings.Contains(page, "nav-count") {
		t.Error("help page shows the old sidebar, the Library source switch or a zero badge")
	}
	if dark := shellGet(t, d, "/help", &http.Cookie{Name: ThemeCookie, Value: "dark"}); !strings.Contains(dark, `<html lang="en" data-theme="dark">`) {
		t.Error("dark theme cookie not applied")
	}
	if odd := shellGet(t, d, "/help", &http.Cookie{Name: ThemeCookie, Value: `x" onload="`}); !strings.Contains(odd, `<html lang="en">`) {
		t.Error("an unknown theme value reached the page")
	}
	// Search itself needs a database; any page rendered as the Search section prefills the box.
	w := httptest.NewRecorder()
	d.render(w, httptest.NewRequest("GET", "/search?q=budget+v2", nil), "help", "Search", "search", map[string]any{})
	if q := w.Body.String(); !strings.Contains(q, `id="header-search" class="input" type="search" name="q" value="budget v2"`) {
		t.Error("header search box not prefilled on Search")
	}
}

func TestShellLibrarySourceSwitch(t *testing.T) {
	boox := shellGet(t, Deps{PublicURL: shellOrigin, Status: Status{NativeBOOX: true}}, "/sources")
	if !strings.Contains(boox, `class="source-switch"`) || !strings.Contains(boox, `href="/library" aria-current="page"`) {
		t.Error("Library pages need the source switch and the Library nav item")
	}
	if client := shellGet(t, Deps{PublicURL: shellOrigin}, "/sources"); strings.Contains(client, `class="source-switch"`) {
		t.Error("with only the Client source there is nothing to switch")
	}
}

func TestThemeToggle(t *testing.T) {
	h := Handler(Deps{PublicURL: shellOrigin})
	post := func(form url.Values, origin string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", shellOrigin+"/settings/theme", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := post(url.Values{"theme": {"dark"}, "back": {"/jobs?state=failed"}}, shellOrigin)
	c := w.Result().Cookies()
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/jobs?state=failed" || len(c) != 1 || c[0].Name != ThemeCookie || c[0].Value != "dark" || !c[0].HttpOnly || c[0].MaxAge <= 0 {
		t.Fatalf("dark: %d %q %+v", w.Code, w.Header().Get("Location"), c)
	}
	if w = post(url.Values{"theme": {"auto"}, "back": {"/"}}, shellOrigin); len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge >= 0 {
		t.Errorf("auto should clear the cookie: %+v", w.Result().Cookies())
	}
	for _, back := range []string{"//evil.test/", `/\evil.test`, "https://evil.test/", "javascript:alert(1)", ""} {
		if w = post(url.Values{"theme": {"light"}, "back": {back}}, shellOrigin); w.Header().Get("Location") != "/" {
			t.Errorf("back %q redirected to %q", back, w.Header().Get("Location"))
		}
	}
	if w = post(url.Values{"theme": {"sepia"}}, shellOrigin); w.Code != http.StatusBadRequest || len(w.Result().Cookies()) != 0 {
		t.Errorf("unknown theme: %d", w.Code)
	}
	if w = post(url.Values{"theme": {"dark"}}, "https://evil.test"); w.Code != http.StatusForbidden {
		t.Errorf("cross-site theme change: %d", w.Code)
	}
}

func TestReturnPathDropsNoticeAndPOSTs(t *testing.T) {
	if got := returnPath(httptest.NewRequest("GET", "/jobs?state=failed&notice=Saved.&notice_n=2", nil)); got != "/jobs?state=failed" {
		t.Errorf("GET: %q", got)
	}
	if got := returnPath(httptest.NewRequest("POST", "/jobs/retry", nil)); got != "/" {
		t.Errorf("POST: %q", got)
	}
}

// Every icon a template names must exist, or the page fails to render.
func TestTemplateIconsExist(t *testing.T) {
	re := regexp.MustCompile(`\{\{icon "([a-z-]+)"`)
	found := 0
	for _, dir := range []fs.FS{assets, os.DirFS("../../boox/templates")} {
		_ = fs.WalkDir(dir, ".", func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, ".html") {
				return err
			}
			b, _ := fs.ReadFile(dir, path)
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				found++
				if _, ok := icons[m[1]]; !ok {
					t.Errorf("%s: unknown icon %q", path, m[1])
				}
			}
			return nil
		})
	}
	if found == 0 {
		t.Fatal("no icons found in templates")
	}
	got, err := icon("books", "big")
	if err != nil || !strings.HasPrefix(string(got), `<svg class="icon big" aria-hidden="true" focusable="false" width="1em" height="1em" `) || !strings.HasSuffix(string(got), "</svg>") {
		t.Errorf("icon markup: %v %.120s", err, got)
	}
	if _, err := icon("nope"); err == nil {
		t.Error("unknown icon accepted")
	}
}
