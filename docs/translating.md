# Translating Alexandria Server

> **Looking for how to use this?** See the guide [Translate Alexandria](guide/translating.md). This page is an engineering note for people working on Alexandria.

The web interface reads its text from go-i18n JSON catalogs in
`internal/alexandria/i18n/locales/`:

- `active.en.json` — English source, generated; don't edit by hand except the
  `one` form of plural messages.
- `active.es.json` — Spanish (first draft by Claude, 2026-10-15; corrections
  welcome).

Each key is the English text; the value is the translation. Weblate and Crowdin
read this format directly ("go-i18n JSON v2").

## Correcting a translation

Edit the value in `active.<lang>.json`. Keep:

- placeholders exactly: `{{.Count}}`, `{{.Title}}`, … (reorder freely);
- inline markup exactly (`<a href="…">`, `<strong>`, `<code>`); translate only the
  text between tags, and leave `<code>` contents alone;
- product and feature names: Alexandria, PowerSync, BOOX, Audio Note Minutes.

The catalog test rejects any translation whose markup (tags and attributes,
including link targets) or placeholders differ from the English message, so a
translation can change wording but never what a page does. Review a
contributed language file like any other change all the same.

Plural messages are objects with one entry per plural form of the language
(Spanish: `one`, `many`, `other`).

## Adding a language

1. Copy `active.es.json` to `active.<code>.json` (for example `active.fr.json`)
   and translate every value.
2. Run `go test ./internal/alexandria/web -run TestCatalogsCoverEveryMessage`.
   It lists anything missing, any plural without its forms, and entries no
   longer used.
3. The language then appears in Settings → Account → Language.

## When English text changes

Rewording English changes the key, so the catalog test fails for every
language until each is updated. Run the test with `I18N_UPDATE=1` to regenerate
the English catalog, then update each language's entry (the test names them).
