// Package providers owns database-backed recognition/search configuration.
package providers

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/minutes"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/settings"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/speech"
)

type OCRConfig struct {
	Enabled                    bool
	URL, Model, Format, Prompt string
	AnthropicWorkspace         string `json:",omitempty"`
	VLLMDisableThinking        bool
}
type EmbedConfig struct {
	Enabled    bool
	URL, Model string
}

// TranscriptionConfig is an OpenAI-compatible /v1/audio/transcriptions endpoint.
type TranscriptionConfig struct {
	Enabled              bool
	URL, Model, Language string
}

// SummaryConfig is the text model that writes note minutes.
type SummaryConfig struct {
	Enabled             bool
	URL, Model, Format  string
	AnthropicWorkspace  string `json:",omitempty"`
	VLLMDisableThinking bool
	// SendToBOOX sends every finished BOOX minutes note to the devices, as
	// if the owner had pressed Send to BOOX.
	SendToBOOX bool `json:",omitempty"`
	// Instructions replace minutes.DefaultInstructions when set. The
	// response format is fixed and always appended.
	Instructions string `json:",omitempty"`
	// NoteLanguage is the language of the headings and labels in minutes
	// notes sent to BOOX: "" for English, "auto" to match the recordings, or
	// a language tag.
	NoteLanguage string `json:",omitempty"`
}

// AutomaticConfig processes new BOOX material without a request: pages are
// recognized and notes with new recordings summarized once they stop changing.
// It is on by default wherever the models are set up; the owner can turn
// either half off.
type AutomaticConfig struct {
	RecognizeOff, SummarizeOff bool `json:",omitempty"`
	// Since is a start time saved by earlier versions, which kept automatic
	// processing opt-in. The start time now lives in the automatic_since
	// setting, set the first time it's needed; this only seeds it.
	Since time.Time
}

// Chosen reports whether the owner leaves any automatic processing on.
func (a AutomaticConfig) Chosen() bool { return !a.RecognizeOff || !a.SummarizeOff }

// AutoRecognize reports whether new BOOX pages are recognized automatically:
// a recognition provider is set up and the owner hasn't turned it off.
func (c Config) AutoRecognize() bool { return c.OCR.Enabled && !c.Automatic.RecognizeOff }

// AutoSummarize reports whether notes with new recordings get minutes
// automatically: transcription and a summary model are set up, and the owner
// hasn't turned it off.
func (c Config) AutoSummarize() bool {
	return c.Transcription.Enabled && c.Summary.Enabled && !c.Automatic.SummarizeOff
}

// ActionsConfig turns BOOX handwriting in assigned pen colors into tasks and
// calendar events. It is on by default: it runs wherever automatic
// recognition and a summary model are both set up, unless turned off.
type ActionsConfig struct {
	Off bool `json:",omitempty"`
	// Red and Blue are "task", "event" or "off"; "" is the default
	// (red makes tasks, blue makes events).
	Red, Blue string `json:",omitempty"`
	// Instructions replace the defaults when set; the response format is
	// fixed and always appended.
	TaskInstructions, EventInstructions string `json:",omitempty"`
}

// For is the action assigned to a pen color ("red" or "blue").
func (a ActionsConfig) For(color string) string {
	v, def := a.Red, "task"
	if color == "blue" {
		v, def = a.Blue, "event"
	}
	if v == "" {
		return def
	}
	return v
}

// ActionsReady reports whether note actions can run with these settings, and why not.
func (c Config) ActionsReady() (bool, string) {
	switch {
	case c.Actions.Off:
		return false, "Note actions are off."
	case !c.OCR.Enabled:
		return false, "Note actions need a recognition provider."
	case !c.Summary.Enabled:
		return false, "Note actions need a summary model."
	case c.Automatic.RecognizeOff:
		return false, "Note actions need automatic recognition."
	}
	return true, ""
}

type Config struct {
	OCR           OCRConfig
	Embedding     EmbedConfig
	Transcription TranscriptionConfig
	Summary       SummaryConfig
	Automatic     AutomaticConfig
	Actions       ActionsConfig `json:",omitempty"`
}

// Bootstrap values seed the database only once. Locked explicitly disables editing.
type Bootstrap struct {
	Key      []byte
	Defaults Config
	APIKey   string
	Locked   bool
}
type Store struct {
	DB        pg.DB
	Bootstrap Bootstrap
	LibraryID string
}
type Snapshot struct {
	Revision int64
	Config   Config
	APIKey   string `json:"-"`
	// Each provider has its own credential; ciphertexts are bound to their purpose.
	TranscriptionKey string `json:"-"`
	SummaryKey       string `json:"-"`
}
type Coverage struct {
	Label                            string
	Texts, Embedded, Pending, Failed int
}
type View struct {
	Coverage                []Coverage
	BOOXWithoutText         int
	SavedEmbedding          EmbedConfig
	Availability            Availability
	IndexedPages            int
	CanResume               bool
	Revision                int64
	Config                  Config
	HasKey, Locked, CanSave bool
	HasTranscriptionKey     bool
	HasSummaryKey           bool
	ActiveModel, IndexState string
	Pending, Failed         int
	// A build in progress: records embedded into it so far and still to go,
	// with its average pace since it started.
	BuildDone, BuildPending int
	BuildPerMinute          float64
	BuildETA                time.Time
}

// BuildTotal is a build's record count, done or not.
func (v View) BuildTotal() int { return v.BuildDone + v.BuildPending }

// BuildPercent is the share of a build's records embedded so far.
func (v View) BuildPercent() int {
	if total := v.BuildTotal(); total > 0 {
		return v.BuildDone * 100 / total
	}
	return 0
}

var ErrStale = errors.New("Settings changed in another session. Reload before saving.")

func (s Store) crypt() (cipher.AEAD, error) {
	if len(s.Bootstrap.Key) != 32 {
		return nil, errors.New("Provider settings encryption key is unavailable.")
	}
	b, e := aes.NewCipher(s.Bootstrap.Key)
	if e != nil {
		return nil, errors.New("Provider settings encryption key is unavailable.")
	}
	return cipher.NewGCM(b)
}

// Purposes bind each sealed credential to its column. The OCR purpose predates
// the others and keeps its original label.
const (
	purposeOCR           = "provider-key-v1"
	purposeTranscription = "transcription-key-v1"
	purposeSummary       = "summary-key-v1"
)

func (s Store) seal(key string, purpose ...string) ([]byte, error) {
	if key == "" {
		return []byte{}, nil
	}
	a, e := s.crypt()
	if e != nil {
		return nil, e
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return a.Seal(n, n, []byte(key), s.aad(purpose)), nil
}
func (s Store) aad(purpose []string) []byte {
	if len(purpose) == 0 {
		return []byte(s.LibraryID + ":" + purposeOCR)
	}
	return []byte(s.LibraryID + ":" + purpose[0])
}
func (s Store) open(b []byte, purpose ...string) (string, error) {
	if len(b) == 0 {
		return "", nil
	}
	a, e := s.crypt()
	if e != nil {
		return "", e
	}
	if len(b) < a.NonceSize() {
		return "", errors.New("Provider credential cannot be decrypted.")
	}
	v, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], s.aad(purpose))
	if e != nil {
		return "", errors.New("Provider credential cannot be decrypted.")
	}
	return string(v), nil
}
func (s Store) Ensure(ctx context.Context) error {
	var exists bool
	if e := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_provider_settings)`).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return nil
	}
	c := s.Bootstrap.Defaults
	if c.OCR.Format == "" {
		c.OCR.Format = "anthropic"
	}
	if c.Embedding.Model == "" {
		c.Embedding.Model = "nomic-embed-text:v1.5"
	}
	if e := Validate(c); e != nil {
		return e
	}
	b, e := s.seal(s.Bootstrap.APIKey)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(c)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `INSERT INTO alexandria_provider_settings(config,credential) VALUES($1,$2) ON CONFLICT DO NOTHING`, raw, b)
	if e != nil {
		return e
	}
	inserted, _ := result.RowsAffected()
	if inserted == 1 && c.Embedding.Enabled {
		id := uuid.NewString()
		if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_embedding_generation(id,endpoint,model,state,dimensions) SELECT $1,$2,$3,'active',max(dimensions) FROM alexandria_embeddings WHERE model=$3`, id, c.Embedding.URL, c.Embedding.Model); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_embedding_vectors(generation,note_key,page,chunk,body_hash,dimensions,embedding) SELECT $1,e.note_key,e.page,e.chunk,md5(c.body_text),e.dimensions,e.embedding FROM alexandria_embeddings e JOIN alexandria_note_content c USING(note_key,page) JOIN alexandria_embedding_generation g ON g.id=$1 WHERE e.model=$2 AND e.dimensions=g.dimensions`, id, c.Embedding.Model); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s Store) Load(ctx context.Context) (Snapshot, error) {
	var v Snapshot
	var raw, b, tb, sb []byte
	e := s.DB.QueryRowContext(ctx, `SELECT revision,config,credential,transcription_credential,summary_credential FROM alexandria_provider_settings WHERE singleton`).Scan(&v.Revision, &raw, &b, &tb, &sb)
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal(raw, &v.Config); e != nil {
		return v, e
	}
	if v.APIKey, e = s.open(b); e != nil {
		return v, e
	}
	if v.TranscriptionKey, e = s.open(tb, purposeTranscription); e != nil {
		return v, e
	}
	v.SummaryKey, e = s.open(sb, purposeSummary)
	return v, e
}
func (s Store) View(ctx context.Context) (View, error) {
	v, e := s.Load(ctx)
	if e != nil {
		return View{}, e
	}
	out := View{SavedEmbedding: v.Config.Embedding, Revision: v.Revision, Config: v.Config, HasKey: v.APIKey != "", HasTranscriptionKey: v.TranscriptionKey != "", HasSummaryKey: v.SummaryKey != "", Locked: s.Bootstrap.Locked, CanSave: len(s.Bootstrap.Key) == 32 && !s.Bootstrap.Locked}
	e = s.DB.QueryRowContext(ctx, `SELECT model FROM alexandria_embedding_generation WHERE state='active'`).Scan(&out.ActiveModel)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	e = s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_embedding_generation WHERE state='building' ORDER BY created_at DESC LIMIT 1`).Scan(&out.IndexState)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if out.IndexState != "" {
		var started, now time.Time
		e = s.DB.QueryRowContext(ctx, `SELECT g.created_at,now(),
 (SELECT count(DISTINCT (v.note_key,v.page)) FROM alexandria_embedding_vectors v WHERE v.generation=g.id),
 (SELECT count(*) FROM alexandria_embedding_work w WHERE w.generation=g.id)
 FROM alexandria_embedding_generation g WHERE g.state='building' ORDER BY g.created_at DESC LIMIT 1`).Scan(&started, &now, &out.BuildDone, &out.BuildPending)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		// An average since the start: a replacement build that reused vectors
		// from the previous index looks faster than it is at first.
		if minutes := now.Sub(started).Minutes(); minutes >= 1 && out.BuildDone > 0 {
			out.BuildPerMinute = float64(out.BuildDone) / minutes
			out.BuildETA = now.Add(time.Duration(float64(out.BuildPending) / out.BuildPerMinute * float64(time.Minute)))
		}
	}
	e = s.DB.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE w.failed) FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation WHERE g.state IN ('active','building')`).Scan(&out.Pending, &out.Failed)
	if e != nil {
		return out, e
	}
	e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM alexandria_embedding_generation WHERE state IN ('active','building') AND endpoint=$1 AND model=$2)`, v.Config.Embedding.URL, v.Config.Embedding.Model).Scan(&out.CanResume)
	if e != nil {
		return out, e
	}
	rows, e := s.DB.QueryContext(ctx, `WITH eligible AS (
 SELECT * FROM alexandria_search_live WHERE btrim(body_text)<>''
 ), kinds(source,kind,label) AS (VALUES ('client','Notebook page','Alexandria Client pages'),('boox','Recognized page','BOOX notebook pages'),('boox','Reading','BOOX reading records'),('boox','Minutes','BOOX Audio Note Minutes'))
 SELECT k.label,
 (SELECT count(*) FROM eligible c WHERE c.source=k.source AND c.kind=k.kind),
 (SELECT count(*) FROM eligible c WHERE c.source=k.source AND c.kind=k.kind AND EXISTS(SELECT 1 FROM alexandria_embedding_vectors v JOIN alexandria_embedding_generation g ON g.id=v.generation AND g.state='active' WHERE v.note_key=c.note_key AND v.page=c.page AND v.body_hash=md5(c.body_text))),
 (SELECT count(DISTINCT (w.note_key,w.page)) FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation AND g.state IN ('active','building') JOIN eligible c ON c.note_key=w.note_key AND c.page=w.page WHERE c.source=k.source AND c.kind=k.kind),
 (SELECT count(DISTINCT (w.note_key,w.page)) FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation AND g.state IN ('active','building') JOIN eligible c ON c.note_key=w.note_key AND c.page=w.page WHERE c.source=k.source AND c.kind=k.kind AND w.failed)
 FROM kinds k ORDER BY k.source DESC,k.kind`)
	if e != nil {
		return out, e
	}
	out.IndexedPages = 0
	for rows.Next() {
		var c Coverage
		if e = rows.Scan(&c.Label, &c.Texts, &c.Embedded, &c.Pending, &c.Failed); e != nil {
			rows.Close()
			return out, e
		}
		out.Coverage = append(out.Coverage, c)
		out.IndexedPages += c.Embedded
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	e = s.DB.QueryRowContext(ctx, `SELECT count(DISTINCT (n.document_id,ids.id)) FROM boox_search_notebooks n CROSS JOIN LATERAL jsonb_array_elements_text(boox_search_page_ids(n.body->'pageNameList')) ids(id) WHERE NOT EXISTS(SELECT 1 FROM boox_search_page_text p WHERE p.document_id=n.document_id AND p.page_id=ids.id)`).Scan(&out.BOOXWithoutText)
	return out, e
}
func endpoint(v string) error {
	if v == "" {
		return nil
	}
	u, e := url.Parse(v)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || len(v) > 2048 {
		return errors.New("Use an HTTP(S) base URL without credentials, query parameters or fragments.")
	}
	return nil
}
func Validate(c Config) error {
	if id := c.OCR.AnthropicWorkspace; id != "" {
		if len(id) > 128 || !strings.HasPrefix(id, "wrkspc_") || len(id) <= len("wrkspc_") {
			return errors.New("Enter a valid Anthropic workspace ID (wrkspc_…).")
		}
		for _, ch := range id {
			if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_') {
				return errors.New("Enter a valid Anthropic workspace ID (wrkspc_…).")
			}
		}
	}

	if e := endpoint(c.OCR.URL); e != nil {
		return e
	}
	if e := endpoint(c.Embedding.URL); e != nil {
		return e
	}
	if e := endpoint(c.Transcription.URL); e != nil {
		return e
	}
	if e := endpoint(c.Summary.URL); e != nil {
		return e
	}
	if id := c.Summary.AnthropicWorkspace; id != "" && (len(id) > 128 || !strings.HasPrefix(id, "wrkspc_") || strings.Trim(id[len("wrkspc_"):], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" || len(id) == len("wrkspc_")) {
		return errors.New("Enter a valid Anthropic workspace ID (wrkspc_…).")
	}
	if c.Summary.Format != "" && c.Summary.Format != "anthropic" && c.Summary.Format != "openai" {
		return errors.New("Choose Anthropic Messages or OpenAI Chat Completions for the summary model.")
	}
	if c.Summary.VLLMDisableThinking && c.Summary.Format != "openai" {
		return errors.New("The vLLM option requires OpenAI format.")
	}
	if c.Transcription.Enabled && (c.Transcription.URL == "" || strings.TrimSpace(c.Transcription.Model) == "") {
		return errors.New("Transcription needs an endpoint and model.")
	}
	if c.Summary.Enabled && (c.Summary.URL == "" || strings.TrimSpace(c.Summary.Model) == "" || c.Summary.Format == "") {
		return errors.New("The summary model needs an endpoint, model and API format.")
	}
	if l := c.Transcription.Language; len(l) > 8 || strings.Trim(l, "abcdefghijklmnopqrstuvwxyz") != "" {
		return errors.New("Leave the language empty to detect it, or enter a code such as en.")
	}
	if utf8.RuneCountInString(c.Summary.Instructions) > minutes.MaxInstructionRunes {
		return errors.New("The summary instructions exceed the settings limit.")
	}
	if len(c.Transcription.Model) > 256 || len(c.Summary.Model) > 256 {
		return errors.New("Model exceeds the settings limit.")
	}
	if c.OCR.Format != "anthropic" && c.OCR.Format != "openai" {
		return errors.New("Choose Anthropic Messages or OpenAI Chat Completions.")
	}
	if c.OCR.VLLMDisableThinking && c.OCR.Format != "openai" {
		return errors.New("The vLLM option requires OpenAI format.")
	}
	if c.OCR.Enabled && (c.OCR.URL == "" || strings.TrimSpace(c.OCR.Model) == "") {
		return errors.New("Recognition needs an endpoint and model.")
	}
	if c.Embedding.Enabled && (c.Embedding.URL == "" || strings.TrimSpace(c.Embedding.Model) == "") {
		return errors.New("Semantic search needs an endpoint and model.")
	}
	if len(c.OCR.Model) > 256 || len(c.Embedding.Model) > 256 || len(c.OCR.Prompt) > 16000 {
		return errors.New("Model or prompt exceeds the settings limit.")
	}
	return nil
}

// editableSnapshot checks deployment locks and the caller's revision. Save
// repeats the revision check under a row lock when committing the edit.
func (s Store) editableSnapshot(ctx context.Context, revision int64) (Snapshot, error) {
	if s.Bootstrap.Locked || len(s.Bootstrap.Key) != 32 {
		return Snapshot{}, errors.New("Provider settings are locked by the deployment.")
	}
	v, e := s.Load(ctx)
	if e != nil {
		return v, e
	}
	if v.Revision != revision {
		return v, ErrStale
	}
	return v, nil
}

func (s Store) Candidate(ctx context.Context, revision int64, c Config, key, action string) (Snapshot, error) {
	v, e := s.editableSnapshot(ctx, revision)
	if e != nil {
		return v, e
	}
	// The recognition and search forms don't carry the minutes providers.
	c.Transcription, c.Summary, c.Actions = v.Config.Transcription, v.Config.Summary, v.Config.Actions
	c.OCR.AnthropicWorkspace = strings.TrimSpace(c.OCR.AnthropicWorkspace)
	c.OCR.URL = strings.TrimRight(strings.TrimSpace(c.OCR.URL), "/")
	c.Embedding.URL = strings.TrimRight(strings.TrimSpace(c.Embedding.URL), "/")
	if e = Validate(c); e != nil {
		return v, e
	}
	if e = keyChange(&v.APIKey, key, action, c.OCR.URL != v.Config.OCR.URL, "recognition"); e != nil {
		return v, e
	}
	v.Config = c
	return v, nil
}

// KeyChange is one provider credential edit from a settings form.
type KeyChange struct{ Key, Action string }

// MinutesCandidate validates the transcription and summary providers, keeping
// the recognition and search settings exactly as saved.
// AutomaticCandidate applies the owner's automatic-processing choices to the
// saved settings. The start time is kept separately (the web handler resets it
// when processing is turned back on), so the legacy one is dropped here.
func (s Store) AutomaticCandidate(ctx context.Context, revision int64, recognize, summarize bool) (Snapshot, error) {
	v, e := s.editableSnapshot(ctx, revision)
	if e != nil {
		return v, e
	}
	v.Config.Automatic = AutomaticConfig{RecognizeOff: !recognize, SummarizeOff: !summarize}
	return v, nil
}

// ActionsCandidate applies the note actions form to the saved settings.
func (s Store) ActionsCandidate(ctx context.Context, revision int64, a ActionsConfig, defaults [2]string) (Snapshot, error) {
	v, e := s.editableSnapshot(ctx, revision)
	if e != nil {
		return v, e
	}
	for _, x := range []*string{&a.Red, &a.Blue} {
		if *x != "task" && *x != "event" && *x != "off" {
			*x = ""
		}
	}
	if a.Red == "task" {
		a.Red = ""
	}
	if a.Blue == "event" {
		a.Blue = ""
	}
	// Blank means the default, so a later change of default reaches owners who never edited it.
	for i, x := range []*string{&a.TaskInstructions, &a.EventInstructions} {
		*x = strings.TrimSpace(strings.ReplaceAll(*x, "\r\n", "\n"))
		if *x == strings.TrimSpace(defaults[i]) {
			*x = ""
		}
		if utf8.RuneCountInString(*x) > minutes.MaxInstructionRunes {
			return v, errors.New("The note action instructions exceed the settings limit.")
		}
	}
	v.Config.Actions = a
	return v, nil
}

func (s Store) MinutesCandidate(ctx context.Context, revision int64, t TranscriptionConfig, m SummaryConfig, tk, mk KeyChange) (Snapshot, error) {
	v, e := s.editableSnapshot(ctx, revision)
	if e != nil {
		return v, e
	}
	t.URL = strings.TrimRight(strings.TrimSpace(t.URL), "/")
	t.Model, t.Language = strings.TrimSpace(t.Model), strings.ToLower(strings.TrimSpace(t.Language))
	m.URL = strings.TrimRight(strings.TrimSpace(m.URL), "/")
	m.Model, m.AnthropicWorkspace = strings.TrimSpace(m.Model), strings.TrimSpace(m.AnthropicWorkspace)
	// The form shows the default text; saving it unchanged keeps following
	// the built-in instructions as they evolve.
	m.Instructions = strings.TrimSpace(strings.ReplaceAll(m.Instructions, "\r\n", "\n"))
	if m.Instructions == minutes.DefaultInstructions {
		m.Instructions = ""
	}
	if m.Format != "anthropic" {
		m.AnthropicWorkspace = ""
	}
	c := v.Config
	c.Transcription, c.Summary = t, m
	if e = Validate(c); e != nil {
		return v, e
	}
	if e = keyChange(&v.TranscriptionKey, tk.Key, tk.Action, t.URL != v.Config.Transcription.URL, "transcription"); e != nil {
		return v, e
	}
	if e = keyChange(&v.SummaryKey, mk.Key, mk.Action, m.URL != v.Config.Summary.URL, "summary"); e != nil {
		return v, e
	}
	v.Config = c
	return v, nil
}

func keyChange(saved *string, key, action string, moved bool, what string) error {
	// Typing a key is an explicit replacement; never silently discard it.
	if action == "keep" && strings.TrimSpace(key) != "" {
		action = "replace"
	}
	switch action {
	case "keep":
		if *saved != "" && moved {
			return fmt.Errorf("Re-enter or clear the API key when changing the %s endpoint.", what)
		}
	case "replace":
		if strings.TrimSpace(key) == "" || len(key) > 8192 {
			return errors.New("Enter a replacement API key.")
		}
		*saved = strings.TrimSpace(key)
	case "clear":
		*saved = ""
	default:
		return errors.New("Invalid API key action.")
	}
	return nil
}
func (s Store) Save(ctx context.Context, v Snapshot) error {
	if s.Bootstrap.Locked || len(s.Bootstrap.Key) != 32 {
		return errors.New("Provider settings are locked by the deployment.")
	}
	if e := Validate(v.Config); e != nil {
		return e
	}
	b, e := s.seal(v.APIKey)
	if e != nil {
		return e
	}
	tb, e := s.seal(v.TranscriptionKey, purposeTranscription)
	if e != nil {
		return e
	}
	sb, e := s.seal(v.SummaryKey, purposeSummary)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(v.Config)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var previousRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT config FROM alexandria_provider_settings WHERE singleton AND revision=$1 FOR UPDATE`, v.Revision).Scan(&previousRaw); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return ErrStale
		}
		return e
	}
	var previous Config
	if e = json.Unmarshal(previousRaw, &previous); e != nil {
		return e
	}

	r, e := tx.ExecContext(ctx, `UPDATE alexandria_provider_settings SET config=$1,credential=$2,transcription_credential=$4,summary_credential=$5,revision=revision+1,updated_at=now() WHERE singleton AND revision=$3`, raw, b, v.Revision, tb, sb)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrStale
	}
	// Clock changes are part of the settings mutation, never a second write
	// from an HTTP adapter after the configuration has already committed.
	var reset []string
	if previous.Automatic.Chosen() != v.Config.Automatic.Chosen() {
		reset = append(reset, settings.AutomaticSince)
	}
	if previous.Actions.Off && !v.Config.Actions.Off {
		reset = append(reset, settings.NoteActionsSince)
	}
	if len(reset) > 0 {
		if _, e = tx.ExecContext(ctx, `DELETE FROM alexandria_settings WHERE key=ANY($1)`, reset); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO alexandria_provider_audit(revision,action) VALUES($1,'provider settings saved')`, v.Revision+1); e != nil {
		return e
	}
	return tx.Commit()
}
func (v Snapshot) OCRClient() *ocr.OCRClient {
	opts := []ocr.Option{ocr.WithAnthropicWorkspace(v.Config.OCR.AnthropicWorkspace)}
	if v.Config.OCR.VLLMDisableThinking {
		opts = append(opts, ocr.WithVLLMDisableThinking())
	}
	return ocr.NewOCRClient(v.Config.OCR.URL, v.APIKey, v.Config.OCR.Model, v.Config.OCR.Format, opts...)
}
func (v Snapshot) SpeechClient() *speech.Client {
	c := v.Config.Transcription
	return speech.New(c.URL, v.TranscriptionKey, c.Model, c.Language)
}

// SummaryClient reuses the recognition transport for text-only completions.
func (v Snapshot) SummaryClient() *ocr.OCRClient {
	c := v.Config.Summary
	opts := []ocr.Option{ocr.WithAnthropicWorkspace(c.AnthropicWorkspace)}
	if c.VLLMDisableThinking {
		opts = append(opts, ocr.WithVLLMDisableThinking())
	}
	return ocr.NewOCRClient(c.URL, v.SummaryKey, c.Model, c.Format, opts...)
}

// TranscriptionIdentity changes when a setting that affects transcripts changes.
func (v Snapshot) TranscriptionIdentity() string {
	c := v.Config.Transcription
	c.Enabled = false
	return identity(c)
}
func identity(c any) string {
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (v Snapshot) OCRIdentity() string {
	c := v.Config.OCR
	c.Enabled = false
	b, _ := json.Marshal(c)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func (s Store) ActiveEmbedder(ctx context.Context) (embed.Embedder, string, error) {
	var id, endpoint, model string
	e := s.DB.QueryRowContext(ctx, `SELECT id,endpoint,model FROM alexandria_embedding_generation WHERE state='active'`).Scan(&id, &endpoint, &model)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, "", nil
	}
	if e != nil {
		return nil, "", fmt.Errorf("embedding configuration unavailable")
	}
	return embed.NewOllama(endpoint, model), id, nil
}
func decodeConfig(raw []byte, c *Config) error { return json.Unmarshal(raw, c) }

// Reprocess is explicit authorization to revisit previously recognized/requested
// pages. Enabling providers or saving a prompt never invokes this action.
func (s Store) Reprocess(ctx context.Context, revision int64) error {
	if s.Bootstrap.Locked {
		return errors.New("Provider settings are locked.")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var rev int64
	var raw []byte
	if e = tx.QueryRowContext(ctx, `SELECT revision,config FROM alexandria_provider_settings WHERE singleton FOR SHARE`).Scan(&rev, &raw); e != nil {
		return e
	}
	if rev != revision {
		return ErrStale
	}
	var c Config
	if e = json.Unmarshal(raw, &c); e != nil {
		return e
	}
	if !c.OCR.Enabled {
		return errors.New("Enable and save recognition first.")
	}
	for _, q := range []string{
		`INSERT INTO alexandria_page_dirty(page_id) SELECT o.page_id FROM alexandria_page_ocr o JOIN fn_page p ON p.id=o.page_id JOIN fn_notebook n ON n.id=p.notebook_id WHERE p.deleted_at IS NULL AND n.deleted_at IS NULL ON CONFLICT(page_id) DO UPDATE SET state='queued',dirtied_at=clock_timestamp(),next_at=now(),lease_until=NULL,attempts=0`,
		`UPDATE boox_page_index p SET state='queued',request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now(),attempts=0,text='',detail='',updated_at=now() FROM boox_projection n WHERE n.document_id=p.notebook_id AND n.body->>'status'='1'`,
	} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit()
}
