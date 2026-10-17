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

// Counted sentences translate with their number as the plural count, and a
// detail made of several stored sentences translates sentence by sentence.
func TestCountedAndJoinedDetailsTranslate(t *testing.T) {
	for lang, cases := range map[string]map[string]string{
		"en": {
			"Made 1 task.":  "Made 1 task.",
			"Made 2 tasks.": "Made 2 tasks.",
			"Made 1 event. 2 already made from this page. 1 without a recognizable date skipped.": "Made 1 event. 2 already made from this page. 1 without a recognizable date skipped.",
			"Recognition provider returned HTTP 503.":                                             "Recognition provider returned HTTP 503.",
		},
		"es": {
			"Made 1 task.":  "Se creó 1 tarea.",
			"Made 2 tasks.": "Se crearon 2 tareas.",
			"Made 1 event. 2 already made from this page. 1 without a recognizable date skipped.":              "Se creó 1 evento. 2 ya se habían creado desde esta página. Se omitió 1 sin una fecha reconocible.",
			"Transcription provider returned HTTP 503.":                                                        "El proveedor de transcripción devolvió HTTP 503.",
			"A recording could not be transcribed: Transcription or page processing failed; waiting to retry.": "No se pudo transcribir una grabación: Falló la transcripción o el procesamiento de la página; se reintentará.",
			"Made 2 tasks. Something a provider said.":                                                         "Se crearon 2 tareas. Something a provider said.",
		},
	} {
		td := detailIn(i18n.For(lang))
		for in, want := range cases {
			if got := td(in); got != want {
				t.Errorf("%s: td(%q) = %q, want %q", lang, in, got, want)
			}
		}
	}
}
