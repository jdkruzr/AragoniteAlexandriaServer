# Translate Alexandria

Alexandria's web app can be shown in more than one language. English is the
original; a Spanish translation is included, and more languages are welcome.
This guide covers choosing a language, correcting or adding a translation
(no programming needed), and, at the end, what developers need to know when
they change English text.

## Choosing a Language

1. Go to **Settings → Account**.
2. Under **Language**, pick from **Show Alexandria in**. **Your browser's
   language** follows your browser's preferences, falling back to English.
3. Select **Save language**.

The choice is saved in this browser. Text that Alexandria writes onto your
tablets is set separately: **Language of headings in minutes notes**, under
**Settings → AI Tools and Recognition**, sets the headings and labels around
Audio Note Minutes (the minutes themselves follow the recordings' language).

A language you add appears in the menu automatically, under its own name (for
example "español").

## For Translators

### The Files

All the text lives in one file per language, in the repository at
`internal/alexandria/i18n/locales/`:

| File | What it is |
|---|---|
| `active.en.json` | The English original. Don't edit it. |
| `active.es.json` | Spanish. |
| `active.<code>.json` | Any other language, named by its language code (`fr`, `de`, `pt-BR`…). |

Each file is a list of entries. The left side (the key) is the English text;
the right side is the translation:

```json
"Add task": "Añadir tarea",
"Search across your library": "Buscar en toda tu biblioteca",
```

Change only the right side. The key must stay exactly as it is, or Alexandria
won't find the translation and will show English.

The format is "go-i18n JSON v2", which translation tools such as Weblate and
Crowdin read directly, if you'd rather not edit JSON by hand.

### Rules

**Placeholders.** Text in double curly braces is filled in by Alexandria:
`{{.Count}}`, `{{.Title}}`, `{{.Zone}}`. Copy them exactly, letter for letter.
You may move them to wherever your grammar needs them:

```json
"Times are in {{.Zone}}. Leave the time empty for an all-day event.":
  "Las horas están en {{.Zone}}. Deja la hora vacía para un evento de todo el día."
```

**Markup.** Some entries contain HTML tags such as `<strong>`, `<em>`,
`<code>` or links (`<a href="/settings/integrations">`). Keep every tag
exactly as in the English, including the link address; translate only the
words between the tags. Leave whatever is inside `<code>…</code>` alone; it's
usually an address or a command.

**Names.** Keep product and feature names as they are: Alexandria,
Alexandria Client, Alexandria Server, PowerSync, BOOX, Audio Note Minutes,
CalDAV, MCP.

**Plurals.** Entries that depend on a number have one line per plural form
instead of a single translation:

```json
"<strong>{{.Count}}</strong> failed": {
  "one": "<strong>{{.Count}}</strong> fallido",
  "many": "<strong>{{.Count}}</strong> fallidos",
  "other": "<strong>{{.Count}}</strong> fallidos"
}
```

Give every form your language uses. `one` and `other` are always required;
languages with more forms (Spanish has `many`; Polish has `few`; and so on)
add theirs. The [Unicode plural rules](https://www.unicode.org/cldr/charts/latest/supplemental/language_plural_rules.html)
list the forms for each language.

**Style.** Match the tone of the English: plain, friendly, short. Headings and
page titles in English use Title Case; follow your own language's convention
for headings instead. Button labels are short commands ("Save", "Add task").

**JSON punctuation.** Keep the quote marks, the colon between key and
translation, and the comma at the end of each line except the last. If your
translation itself contains a double quote, write it as `\"`; typographic
quotes (« », „ ", “ ”) need no escaping.

The automatic check (below) rejects a translation whose tags, link addresses
or placeholders differ from the English, so a translation can change wording
but never what a page does.

### Correcting a Translation

1. Find the entry in `active.<code>.json` (search the file for the English
   text you saw on screen).
2. Change the right-hand side.
3. Submit it (see below).

### Adding a Language

1. Copy `active.es.json` to `active.<code>.json`, for example
   `active.fr.json`.
2. Replace every Spanish value with your translation. For plural entries,
   use your language's forms and remove any Spanish forms your language
   doesn't have.
3. Submit it. Once it's merged and the server is rebuilt, the language
   appears under **Settings → Account → Language**.

A partial translation is still useful to send: say which parts are done and
someone can help finish it. The check reports missing entries by name.

### Submitting Your Work

The project lives at
[github.com/jdkruzr/AragoniteAlexandriaServer](https://github.com/jdkruzr/AragoniteAlexandriaServer).

**With a pull request** (preferred):

1. Fork the repository on GitHub.
2. Edit or add the file under `internal/alexandria/i18n/locales/`. GitHub's
   web editor is fine for small corrections.
3. Open a pull request describing the language and what you changed.
4. The project's automatic checks run on every pull request and report
   missing entries, missing plural forms, and changed tags or placeholders.
   Fix anything they list and push again.

**Without Git:** open an issue on the repository, attach your file (or paste
the corrected lines), and say which language it is.

### Checking Your File Yourself

If you have Docker, you can run the same check locally from a copy of the
repository:

```sh
docker run --rm -v "$PWD":/src -w /src golang:1.25 \
  go test ./internal/alexandria/web -run TestCatalogsCoverEveryMessage
```

It prints `ok` when everything matches, or one line per problem: an entry that
is missing, a plural without its forms, an entry whose markup or placeholders
differ from English, or an entry that is no longer used.

### Reviewing the Spanish Translation

The Spanish translation is a first draft and native speakers' corrections are
very welcome. Useful things to look for:

- Wording that is correct but stiff, or reads like a literal translation.
- Inconsistent terms for the same thing (for example, two different words for
  "notebook" or "recognition").
- Text that is too long for a button.
- Regional usage, and whether the form of address (tú or usted) is consistent.

To review in context, set **Settings → Account → Language** to español and
walk through the pages; fix entries as described above.

## For Developers

The catalogs are go-i18n JSON keyed by English text (gettext style); a missing
entry falls back to English. Messages come from the web and BOOX templates
(`{{t "…"}}`, and `{{th "…"}}` for messages containing markup) and from Go code
(`i18n.M`), including the help tips.

- `TestCatalogsCoverEveryMessage` in `internal/alexandria/web` checks every
  catalog against the messages in use: nothing missing, plural messages with
  `one` and `other`, markup and placeholders identical to English, and no
  unused entries.
- Rewording English changes the key, so the test fails for every language
  until each is updated. Run it with `I18N_UPDATE=1` to regenerate
  `active.en.json` from current use, then update each language's entries (the
  test names them). Hand-edit only the `one` form of English plural messages;
  regeneration keeps it.
- `scripts/i18n-wrap.py [--dry-run] FILE...` wraps plain text and
  `aria-label`/`placeholder`/`title`/`alt` attributes in templates with
  `{{t "…"}}`. Text next to a printed value is reported rather than wrapped;
  rewrite those by hand with placeholders so translators can reorder words.
- `scripts/i18n-merge.py FILE...` then merges sentence fragments split by
  inline markup into a single `{{th "…"}}` message, so translators see whole
  sentences.
- Catalogs are embedded at build time; a new or changed language needs a
  rebuilt image.
