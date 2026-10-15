// Package i18n translates owner-facing text. Messages are identified by their
// English source text (gettext style), so templates stay readable and a
// missing translation falls back to English. Catalogs are go-i18n JSON files
// in locales/, the format Weblate and Crowdin read directly.
package i18n

import (
	"embed"
	"encoding/json"
	"strings"
	"text/template"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

//go:embed locales/*.json
var locales embed.FS

var bundle = load()

func load() *goi18n.Bundle {
	b := goi18n.NewBundle(language.English)
	b.RegisterUnmarshalFunc("json", json.Unmarshal)
	entries, _ := locales.ReadDir("locales")
	for _, e := range entries {
		if _, err := b.LoadMessageFileFS(locales, "locales/"+e.Name()); err != nil {
			panic("i18n: " + e.Name() + ": " + err.Error())
		}
	}
	return b
}

var matcher = language.NewMatcher(Languages())

// Languages lists the available languages, English first.
func Languages() []language.Tag { return bundle.LanguageTags() }

// Localizer renders messages in one language.
type Localizer struct {
	Tag language.Tag
	l   *goi18n.Localizer
}

// For picks the best available language for the preferences given, each a
// language tag or an Accept-Language header, most important first.
func For(preferences ...string) Localizer {
	var tags []language.Tag
	for _, p := range preferences {
		if t, _, err := language.ParseAcceptLanguage(p); err == nil {
			tags = append(tags, t...)
		}
	}
	tag := language.English
	if len(tags) > 0 {
		_, i, conf := matcher.Match(tags...)
		if conf != language.No {
			tag = Languages()[i]
		}
	}
	return Localizer{Tag: tag, l: goi18n.NewLocalizer(bundle, tag.String())}
}

// English is the source-language localizer.
var English = For("en")

// T translates id, the English source text. With one argument it is the
// plural count, available as {{.Count}}; otherwise arguments are name/value
// pairs, available as {{.Name}}, and a "Count" pair picks the plural form.
func (l Localizer) T(id string, args ...any) string {
	cfg := &goi18n.LocalizeConfig{MessageID: id, DefaultMessage: &goi18n.Message{ID: id, Other: id}}
	data := map[string]any{}
	if len(args) == 1 {
		cfg.PluralCount = args[0]
		data["Count"] = args[0]
	} else {
		for i := 0; i+1 < len(args); i += 2 {
			if k, ok := args[i].(string); ok {
				data[k] = args[i+1]
			}
		}
		if n, ok := data["Count"]; ok {
			cfg.PluralCount = n
		}
	}
	cfg.TemplateData = data
	if l.l != nil {
		if s, err := l.l.Localize(cfg); err == nil {
			return s
		}
	}
	return fallback(id, data)
}

// fallback renders the English source text when no catalog entry applies.
func fallback(id string, data map[string]any) string {
	if !strings.Contains(id, "{{") {
		return id
	}
	t, err := template.New("").Option("missingkey=zero").Parse(id)
	if err != nil {
		return id
	}
	var b strings.Builder
	if t.Execute(&b, data) != nil {
		return id
	}
	return b.String()
}

// Lang is the tag for an HTML lang attribute.
func (l Localizer) Lang() string { return l.Tag.String() }

// M marks English text in Go source as a message to translate where it is
// shown (gettext's N_). It returns s unchanged; the catalog tooling finds it.
func M(s string) string { return s }

// Choice is a selectable language, named in itself ("Español").
type Choice struct{ Tag, Name string }

// Choices lists the available languages for a language picker.
func Choices() []Choice {
	var out []Choice
	for _, t := range Languages() {
		name := display.Self.Name(t)
		if name == "" {
			name = t.String()
		}
		out = append(out, Choice{t.String(), strings.ToUpper(name[:1]) + name[1:]})
	}
	return out
}

// Supported reports whether tag names one of the available languages exactly.
func Supported(tag string) bool {
	for _, t := range Languages() {
		if t.String() == tag {
			return true
		}
	}
	return false
}

var known = loadKnown()

func loadKnown() map[string]bool {
	m := map[string]any{}
	if b, err := locales.ReadFile("locales/active.en.json"); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

// Known reports whether id is a message in the English source catalog.
func Known(id string) bool { return known[id] }
