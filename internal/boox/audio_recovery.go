package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

const audioTransform = "ony1-remove-duplicate-iv-v1"
const maxRecoveryAudio = 16 << 20

type audioRecoveryItem struct {
	ID       string `json:"operationId"`
	Document string `json:"documentId"`
	Title    string `json:"title"`
	File     string `json:"fileName"`
}

// Only explicitly published repairs, in the current generation, whose derived
// asset still wins can be offered to a companion. Device auth is handled outside.
const recoverableAudioSQL = ` FROM boox_operation o
 JOIN sync_library_generation g ON g.id=1 AND g.generation::text=o.payload->>'generation'
 JOIN boox_projection p ON p.document_id=o.document_id
 JOIN boox_blob_live l ON l.native_key=o.payload->>'nativeKey' AND l.version_id::text=o.payload->>'versionId'
 JOIN boox_blob_version v ON v.id=l.version_id AND NOT v.deleted AND v.sha256=o.payload->>'sha256'
 WHERE o.kind='repair_asset' AND o.state='published' AND o.payload->>'transform'='ony1-remove-duplicate-iv-v1'
 AND p.native_uid=$1 AND p.body->>'user'=$1 AND p.body->>'dbId'=$1||'-NOTE_TREE'
 AND p.body->>'type'='1' AND p.body->>'status'='1' AND p.body->>'uniqueId'=p.document_id
 AND starts_with(l.native_key,$1||'/note/'||p.document_id||'/resource/data/')
 AND right(l.native_key,4)='.mp3' AND v.bytes <= 16778272`

func (s Service) audioRecoveryDevice(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	id := r.URL.Query().Get("operation")
	if id == "" {
		after := r.URL.Query().Get("after")
		if after != "" {
			if _, err := uuid.Parse(after); err != nil {
				http.Error(w, "Invalid cursor", 400)
				return
			}
		}
		rows, err := s.DB.QueryContext(r.Context(), `SELECT o.id::text,p.document_id,coalesce(p.body->>'title',''),l.native_key`+recoverableAudioSQL+` AND o.id::text>$2 ORDER BY o.id::text LIMIT 51`, s.uid(), after)
		if err != nil {
			failure(w, err)
			return
		}
		defer rows.Close()
		items := []audioRecoveryItem{}
		for rows.Next() {
			var v audioRecoveryItem
			if err = rows.Scan(&v.ID, &v.Document, &v.Title, &v.File); err != nil {
				failure(w, err)
				return
			}
			v.File = path.Base(v.File)
			items = append(items, v)
		}
		if err = rows.Err(); err != nil {
			failure(w, err)
			return
		}
		next := ""
		if len(items) > 50 {
			items = items[:50]
			next = items[49].ID
		}
		reply(w, 200, map[string]any{"nativeUid": s.uid(), "items": items, "next": next})
		return
	}
	if _, err := uuid.Parse(id); err != nil {
		http.NotFound(w, r)
		return
	}
	packet, err := s.audioRecoveryPacket(r.Context(), id)
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "Audio recovery is no longer available. Refresh the list.", 409)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	reply(w, 200, packet)
}

func (s Service) audioRecoveryPacket(ctx context.Context, id string) (map[string]any, error) {
	var document, key, sourceSHA, derivedSHA string
	var size int64
	err := s.DB.QueryRowContext(ctx, `SELECT p.document_id,l.native_key,src.sha256,v.sha256,src.bytes`+
		strings.Replace(recoverableAudioSQL, " WHERE ", ` JOIN boox_blob_version src ON src.id::text=o.payload->>'sourceVersion' AND src.native_key=l.native_key AND NOT src.deleted AND src.sha256=o.payload->>'sourceSHA256' WHERE `, 1)+` AND o.id::text=$2`, s.uid(), id).Scan(&document, &key, &sourceSHA, &derivedSHA, &size)
	if err != nil {
		return nil, err
	}
	if size > maxRecoveryAudio+1056 {
		return nil, sql.ErrNoRows
	}
	body, _, err := s.Objects.Get(ctx, "boox/bodies/"+sourceSHA)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(body, maxRecoveryAudio+1057))
	body.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) != size || hash(raw) != sourceSHA {
		return nil, errors.New("audio source integrity failure")
	}
	fixed, original, damaged, err := audioEnvelope(raw)
	if err != nil {
		return nil, err
	}
	if hash(fixed) != derivedSHA || len(original) > maxRecoveryAudio {
		return nil, errors.New("audio derivation integrity failure")
	}
	return map[string]any{"operationId": id, "nativeUid": s.uid(), "documentId": document, "fileName": path.Base(key), "sha256": hash(original), "damagedSHA256": hash(damaged), "bytes": len(original), "content": original}, nil
}

type audioRecoveryView struct {
	Document, Title, Revision, Key, Operation, State, Error string
	Items                                                   []audioRecoveryAsset
}
type audioRecoveryAsset struct {
	Key, Name      string
	Version, Bytes int64
}

var audioRecoveryTemplate = template.Must(template.New("audio-recovery").Parse(`{{define "content"}}
<p class="crumbs"><a href="/boox/notebook?id={{.Data.Document}}">{{.Data.Title}}</a> /</p><h1>Recover Notebook Audio</h1>
<p class="lede">Repairs recordings affected by a known BOOX audio defect, one recording at a time. The original stays on the server.</p>
{{if .Data.Error}}<p role="alert">{{.Data.Error}}</p>{{end}}
{{if .Data.Operation}}<p>Server repair: <strong>{{.Data.State}}</strong></p>
{{if eq .Data.State "preview"}}<p>The selected recording passed the compatibility checks. Publishing replaces its cloud download with a corrected copy.</p><form method="post"><input type="hidden" name="documentId" value="{{.Data.Document}}"><input type="hidden" name="operationId" value="{{.Data.Operation}}"><button>Publish corrected audio</button></form>{{end}}
{{if eq .Data.State "published"}}<p>For an MP3 already on a device, stop recording and close the notebook, then open PowerSync and choose <strong>Recover notebook audio</strong>. PowerSync preserves a local backup and replaces only a file that exactly matches the known defect. MP3s up to 16 MiB are supported; PCM cache recovery is not yet supported.</p><p>Device playback and application are not confirmed by this server status.</p>{{end}}
<p><a href="/boox/audio?document={{.Data.Document}}&amp;operation={{.Data.Operation}}">Refresh repair status</a> · <a href="/boox/audio?document={{.Data.Document}}">Review another recording</a></p>
{{else}}<p class="muted">A preview checks the full retained recording before making any change to cloud downloads. An already corrected or unsupported recording is rejected.</p>
{{range .Data.Items}}<section class="card"><p>{{.Name}} · {{.Bytes}} stored bytes</p><form method="post"><input type="hidden" name="documentId" value="{{$.Data.Document}}"><input type="hidden" name="expectedRevision" value="{{$.Data.Revision}}"><input type="hidden" name="nativeKey" value="{{.Key}}"><input type="hidden" name="versionId" value="{{.Version}}"><input type="hidden" name="idempotencyKey" value="{{$.Data.Key}}"><button>Preview audio repair</button></form></section>{{else}}<p>No live audio resources are available for this notebook.</p>{{end}}{{end}}
{{end}}`))

func (s Service) audioRecoveryHTTP(w http.ResponseWriter, r *http.Request) {
	d := audioRecoveryView{Document: r.URL.Query().Get("document"), Operation: r.URL.Query().Get("operation"), Key: uuid.NewString()}
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid audio request", 400)
			return
		}
		d.Document = r.PostForm.Get("documentId")
		d.Operation = r.PostForm.Get("operationId")
		if d.Operation != "" {
			if _, err := uuid.Parse(d.Operation); err != nil {
				http.Error(w, "Invalid repair", 400)
				return
			}
			result, err := s.DB.ExecContext(r.Context(), `UPDATE boox_operation SET state='queued',updated_at=now() WHERE id=$1 AND document_id=$2 AND state='preview' AND kind='repair_asset' AND payload->>'transform'=$3`, d.Operation, d.Document, audioTransform)
			if err != nil {
				failure(w, err)
				return
			}
			n, _ := result.RowsAffected()
			if n != 1 {
				http.Error(w, "Repair is no longer awaiting confirmation. Refresh its status.", 409)
				return
			}
		} else {
			v, err := strconv.ParseInt(r.PostForm.Get("versionId"), 10, 64)
			if err != nil || v < 1 {
				http.Error(w, "Invalid recording", 400)
				return
			}
			result, err := s.previewAudioRepair(r.Context(), audioRepairInput{DocumentID: d.Document, Expected: r.PostForm.Get("expectedRevision"), Key: r.PostForm.Get("nativeKey"), Version: v, Idempotency: r.PostForm.Get("idempotencyKey")})
			if err != nil {
				var rejected audioRepairFailure
				if errors.As(err, &rejected) {
					http.Error(w, rejected.message, rejected.status)
				} else {
					failure(w, err)
				}
				return
			}
			d.Operation = result["operationId"].(string)
		}
		http.Redirect(w, r, "/boox/audio?"+url.Values{"document": {d.Document}, "operation": {d.Operation}}.Encode(), 303)
		return
	}
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	var raw []byte
	err := s.DB.QueryRowContext(r.Context(), `SELECT revision,body FROM boox_projection WHERE document_id=$1 AND native_uid=$2`, d.Document, s.uid()).Scan(&d.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		failure(w, err)
		return
	}
	var b map[string]any
	if nativeJSON(raw, &b) != nil || !noteMetadata(b, s.uid()) || b["status"] != json.Number("1") {
		http.NotFound(w, r)
		return
	}
	d.Title, _ = b["title"].(string)
	if d.Operation != "" {
		if _, err = uuid.Parse(d.Operation); err != nil {
			http.NotFound(w, r)
			return
		}
		err = s.DB.QueryRowContext(r.Context(), `SELECT state FROM boox_operation WHERE id=$1 AND document_id=$2 AND kind='repair_asset' AND payload->>'transform'=$3`, d.Operation, d.Document, audioTransform).Scan(&d.State)
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			failure(w, err)
			return
		}
		if d.State == "blocked" || d.State == "superseded" {
			d.Error = "The notebook or recording changed. Review it again before requesting a new repair."
		}
	} else {
		rows, err := s.DB.QueryContext(r.Context(), `SELECT l.native_key,v.id,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE NOT v.deleted AND starts_with(l.native_key,$1) AND lower(right(l.native_key,4)) IN ('.mp3','.pcm') ORDER BY l.native_key`, s.uid()+"/note/"+d.Document+"/resource/data/")
		if err != nil {
			failure(w, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var a audioRecoveryAsset
			if err = rows.Scan(&a.Key, &a.Version, &a.Bytes); err != nil {
				failure(w, err)
				return
			}
			a.Name = path.Base(a.Key)
			d.Items = append(d.Items, a)
		}
		if err = rows.Err(); err != nil {
			failure(w, err)
			return
		}
	}
	s.renderPage(w, r, audioRecoveryTemplate, "Recover Notebook Audio", d)
}
