package web

import (
	"testing"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/i18n"
)

// Stored English details show translated when they're known sentences or
// known "prefix: reason" shapes; anything else is shown exactly as stored.
func TestStoredDetailsTranslate(t *testing.T) {
	td := detailIn(i18n.For("es"))
	for in, want := range map[string]string{
		"No handwriting on this page; nothing was sent for recognition.":                           "No hay escritura en esta página; no se envió nada a reconocimiento.",
		"Not sent for recognition because the preview is incomplete: Invalid wave geometry.":       "No se envió a reconocimiento porque la vista previa está incompleta: Geometría de onda no válida.",
		"Not sent for recognition because the preview is incomplete: Native shape type 24 is odd.": "No se envió a reconocimiento porque la vista previa está incompleta: Native shape type 24 is odd.",
		"provider said: 429 Too Many Requests":                                                     "provider said: 429 Too Many Requests",
	} {
		if got := td(in); got != want {
			t.Errorf("td(%q) = %q, want %q", in, got, want)
		}
	}
	if got := detailIn(i18n.English)("Owner requested cancel."); got != "Owner requested cancel." {
		t.Errorf("English changed: %q", got)
	}
}
