package web

import (
	"testing"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// Numbers and relative times follow the reader's language; English output is
// what the pages showed before localization.
func TestFormatsFollowLanguage(t *testing.T) {
	en, es := formatFuncs(i18n.English), formatFuncs(i18n.For("es"))
	call := func(f map[string]any, name string, args ...any) string {
		switch fn := f[name].(type) {
		case func(int) string:
			return fn(args[0].(int))
		case func(int64) string:
			return fn(args[0].(int64))
		case func(float64, int) string:
			return fn(args[0].(float64), args[1].(int))
		case func(time.Time) string:
			return fn(args[0].(time.Time))
		}
		t.Fatalf("unexpected %s", name)
		return ""
	}
	for _, c := range []struct {
		name   string
		args   []any
		en, es string
	}{
		{"num", []any{529070}, "529,070", "529.070"},
		{"num", []any{1625}, "1,625", "1625"},
		{"count", []any{-1}, "—", "—"},
		{"bytes", []any{int64(1572864)}, "1.5 MB", "1,5 MB"},
		{"bytes", []any{int64(2048)}, "2 KB", "2 KB"},
		{"decimal", []any{12.25, 1}, "12.2", "12,2"},
		{"ago", []any{time.Now().Add(-3 * time.Minute)}, "3 minutes ago", "hace 3 minutos"},
		{"ago", []any{time.Now().Add(-61 * time.Minute)}, "1 hour ago", "hace 1 hora"},
		{"ago", []any{time.Time{}}, "never", "nunca"},
	} {
		if got := call(en, c.name, c.args...); got != c.en {
			t.Errorf("en %s%v = %q, want %q", c.name, c.args, got, c.en)
		}
		if got := call(es, c.name, c.args...); got != c.es {
			t.Errorf("es %s%v = %q, want %q", c.name, c.args, got, c.es)
		}
	}
	if got := i18n.For("es").T("Recognition image: {{.Width}} × {{.Height}} px · {{.Bytes}} JPEG bytes", "Width", 1860, "Height", 2480, "Bytes", 529070); got != "Imagen de reconocimiento: 1860 × 2480 px · 529.070 bytes JPEG" {
		t.Errorf("numbers inside messages: %q", got)
	}
}
