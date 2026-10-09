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

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/embed"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/ocr"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
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
type Config struct {
	OCR       OCRConfig
	Embedding EmbedConfig
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
}
type View struct {
	Revision                int64
	Config                  Config
	HasKey, Locked, CanSave bool
	ActiveModel, IndexState string
	Pending, Failed         int
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
func (s Store) seal(key string) ([]byte, error) {
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
	return a.Seal(n, n, []byte(key), []byte(s.LibraryID+":provider-key-v1")), nil
}
func (s Store) open(b []byte) (string, error) {
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
	v, e := a.Open(nil, b[:a.NonceSize()], b[a.NonceSize():], []byte(s.LibraryID+":provider-key-v1"))
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
	var raw, b []byte
	e := s.DB.QueryRowContext(ctx, `SELECT revision,config,credential FROM alexandria_provider_settings WHERE singleton`).Scan(&v.Revision, &raw, &b)
	if e != nil {
		return v, e
	}
	if e = json.Unmarshal(raw, &v.Config); e != nil {
		return v, e
	}
	v.APIKey, e = s.open(b)
	return v, e
}
func (s Store) View(ctx context.Context) (View, error) {
	v, e := s.Load(ctx)
	if e != nil {
		return View{}, e
	}
	out := View{Revision: v.Revision, Config: v.Config, HasKey: v.APIKey != "", Locked: s.Bootstrap.Locked, CanSave: len(s.Bootstrap.Key) == 32 && !s.Bootstrap.Locked}
	e = s.DB.QueryRowContext(ctx, `SELECT model FROM alexandria_embedding_generation WHERE state='active'`).Scan(&out.ActiveModel)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	e = s.DB.QueryRowContext(ctx, `SELECT state FROM alexandria_embedding_generation WHERE state='building' ORDER BY created_at DESC LIMIT 1`).Scan(&out.IndexState)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	e = s.DB.QueryRowContext(ctx, `SELECT count(*),count(*) FILTER(WHERE w.failed) FROM alexandria_embedding_work w JOIN alexandria_embedding_generation g ON g.id=w.generation WHERE g.state IN ('active','building')`).Scan(&out.Pending, &out.Failed)
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
func (s Store) Candidate(ctx context.Context, revision int64, c Config, key, action string) (Snapshot, error) {
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
	c.OCR.AnthropicWorkspace = strings.TrimSpace(c.OCR.AnthropicWorkspace)
	c.OCR.URL = strings.TrimRight(strings.TrimSpace(c.OCR.URL), "/")
	c.Embedding.URL = strings.TrimRight(strings.TrimSpace(c.Embedding.URL), "/")
	if e = Validate(c); e != nil {
		return v, e
	}
	// Typing a key is an explicit replacement; never silently discard it.
	if action == "keep" && strings.TrimSpace(key) != "" {
		action = "replace"
	}
	switch action {
	case "keep":
		if v.APIKey != "" && c.OCR.URL != v.Config.OCR.URL {
			return v, errors.New("Re-enter or clear the API key when changing the recognition endpoint.")
		}
	case "replace":
		if strings.TrimSpace(key) == "" || len(key) > 8192 {
			return v, errors.New("Enter a replacement API key.")
		}
		v.APIKey = strings.TrimSpace(key)
	case "clear":
		v.APIKey = ""
	default:
		return v, errors.New("Invalid API key action.")
	}
	v.Config = c
	return v, nil
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
	raw, _ := json.Marshal(v.Config)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE alexandria_provider_settings SET config=$1,credential=$2,revision=revision+1,updated_at=now() WHERE singleton AND revision=$3`, raw, b, v.Revision)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrStale
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
		`INSERT INTO alexandria_page_dirty(page_id) SELECT o.page_id FROM alexandria_page_ocr o JOIN fn_page p ON p.id=o.page_id JOIN fn_notebook n ON n.id=p.notebook_id WHERE p.deleted_at IS NULL AND n.deleted_at IS NULL ON CONFLICT(page_id) DO UPDATE SET dirtied_at=clock_timestamp(),next_at=now(),lease_until=NULL,attempts=0`,
		`UPDATE boox_page_index p SET state='queued',request_version=request_version+1,lease_token='',lease_until=NULL,next_at=now(),attempts=0,text='',detail='',updated_at=now() FROM boox_projection n WHERE n.document_id=p.notebook_id AND n.body->>'status'='1'`,
	} {
		if _, e = tx.ExecContext(ctx, q); e != nil {
			return e
		}
	}
	return tx.Commit()
}
