package boox

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

const cleanupLimit = 200
const cleanupPredicate = notebookPredicate + ` AND native_uid=$1 AND body->>'user'=$1 AND body->>'dbId'=$1||'-NOTE_TREE' AND body->>'_id'=document_id AND body->>'type'='1' AND body->>'status'='1' AND body->>'title' ~ '^Notebook-[0-9]+$'`

var cleanupName = regexp.MustCompile(`^Notebook-[0-9]+$`)
var errCleanupSelection = errors.New("The selection changed or is unavailable. Review the current matches again; nothing from this submission was queued.")

type cleanupChoice struct{ ID, Revision string }
type cleanupItem struct {
	ID, Title, Choice, State, Detail string
	Created, Modified                int64
	Pages                            int
	Truncated                        bool
}
type cleanupBatch struct {
	ID      string
	Created time.Time
	Total   int
}
type cleanupPage struct {
	Token, Batch                                  string
	Total, Published, Blocked, Pending, Cancelled int
	Items                                         []cleanupItem
	Recent                                        []cleanupBatch
}

func (s Service) cleanupCandidates(ctx context.Context) (cleanupPage, error) {
	d := cleanupPage{Token: uuid.NewString()}
	rows, e := s.DB.QueryContext(ctx, `SELECT document_id,revision,body,count(*) OVER() FROM boox_projection WHERE `+cleanupPredicate+` ORDER BY body->>'title',document_id LIMIT $2`, s.uid(), cleanupLimit)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	for rows.Next() {
		var v cleanupItem
		var revision string
		var raw []byte
		if e = rows.Scan(&v.ID, &revision, &raw, &d.Total); e != nil {
			return d, e
		}
		var m struct {
			Title                string
			CreatedAt, UpdatedAt int64
			PageNameList         json.RawMessage
		}
		if e = json.Unmarshal(raw, &m); e != nil {
			return d, e
		}
		v.Title, v.Created, v.Modified = m.Title, m.CreatedAt, m.UpdatedAt
		v.Pages = len(booxpage.PageIDs(m.PageNameList))
		v.Truncated = v.Pages >= 500
		choice, _ := json.Marshal(cleanupChoice{v.ID, revision})
		v.Choice = string(choice)
		d.Items = append(d.Items, v)
	}
	return d, rows.Err()
}

// A form is one durable intent. All selected revisions are checked and inserted
// atomically; publication checks the authoritative Gateway revisions again.
func (s Service) queueCleanup(ctx context.Context, token string, choices []cleanupChoice) error {
	if _, e := uuid.Parse(token); e != nil {
		return errCleanupSelection
	}
	if len(choices) == 0 || len(choices) > cleanupLimit {
		return errCleanupSelection
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].ID < choices[j].ID })
	for i, c := range choices {
		if c.ID == "" || c.Revision == "" || (i > 0 && choices[i-1].ID == c.ID) {
			return errCleanupSelection
		}
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	// Serialize double clicks and concurrent replays, including before first insert.
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "boox-cleanup:"+token); e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM boox_operation WHERE payload->>'cleanupBatch'=$1)`, token).Scan(&exists); e != nil {
		return e
	}
	if exists {
		return tx.Commit()
	}
	var generation string
	if e = tx.QueryRowContext(ctx, `SELECT generation FROM sync_library_generation WHERE id=1 FOR SHARE`).Scan(&generation); e != nil {
		return e
	}
	for _, c := range choices {
		var raw []byte
		var revision string
		e = tx.QueryRowContext(ctx, `SELECT body,revision FROM boox_projection WHERE document_id=$2 AND `+cleanupPredicate+` FOR SHARE`, s.uid(), c.ID).Scan(&raw, &revision)
		if errors.Is(e, sql.ErrNoRows) {
			return errCleanupSelection
		}
		if e != nil {
			return e
		}
		var body map[string]any
		if nativeJSON(raw, &body) != nil || revision != c.Revision || !noteMetadata(body, s.uid()) || !cleanupName.MatchString(fmt.Sprint(body["title"])) {
			return errCleanupSelection
		}
		payload, _ := json.Marshal(map[string]any{"cleanupBatch": token, "cleanupTitle": body["title"], "status": 0, "generation": generation, "updatedAt": time.Now().UnixMilli()})
		id := uuid.NewSHA1(uuid.NameSpaceOID, []byte("boox-cleanup:"+token+":"+c.ID)).String()
		if _, e = tx.ExecContext(ctx, `INSERT INTO boox_operation(id,idempotency_key,kind,document_id,expected_revision,payload,state) VALUES($1,$2,'set_note_status',$3,$4,$5,'queued')`, id, "cleanup:"+id, c.ID, c.Revision, payload); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (s Service) cleanupStatus(ctx context.Context, id string) (cleanupPage, error) {
	d := cleanupPage{Batch: id}
	if _, e := uuid.Parse(id); e != nil {
		return d, sql.ErrNoRows
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT document_id,payload->>'cleanupTitle',state,coalesce(error_code,'') FROM boox_operation WHERE payload->>'cleanupBatch'=$1 ORDER BY payload->>'cleanupTitle',document_id`, id)
	if e != nil {
		return d, e
	}
	defer rows.Close()
	for rows.Next() {
		var v cleanupItem
		if e = rows.Scan(&v.ID, &v.Title, &v.State, &v.Detail); e != nil {
			return d, e
		}
		switch v.State {
		case "published":
			d.Published++
		case "blocked", "superseded":
			d.Blocked++
		case "cancelled":
			d.Cancelled++
		default:
			d.Pending++
		}
		d.Items = append(d.Items, v)
		d.Total++
	}
	if e = rows.Err(); e != nil {
		return d, e
	}
	if d.Total == 0 {
		return d, sql.ErrNoRows
	}
	return d, nil
}
func (s Service) cleanupHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid cleanup form", 400)
			return
		}
		token := r.PostForm.Get("token")
		if r.PostForm.Get("action") == "cancel" {
			if _, e := uuid.Parse(token); e != nil {
				http.Error(w, "Invalid cleanup batch", 400)
				return
			}
			_, e := s.DB.ExecContext(r.Context(), `UPDATE boox_operation SET state='cancelled',updated_at=now() WHERE payload->>'cleanupBatch'=$1 AND state='queued'`, token)
			if e != nil {
				failure(w, e)
				return
			}
		} else if r.PostForm.Get("action") == "queue" {
			if r.PostForm.Get("confirm") != "on" {
				http.Error(w, "Confirm deletion on all synced BOOX devices before continuing.", 400)
				return
			}
			var choices []cleanupChoice
			for _, value := range r.PostForm["selected"] {
				var c cleanupChoice
				if json.Unmarshal([]byte(value), &c) != nil {
					http.Error(w, "Invalid notebook selection", 400)
					return
				}
				choices = append(choices, c)
			}
			e := s.queueCleanup(r.Context(), token, choices)
			if errors.Is(e, errCleanupSelection) {
				http.Error(w, e.Error(), 409)
				return
			}
			if e != nil {
				failure(w, e)
				return
			}
		} else {
			http.Error(w, "Unknown cleanup action", 400)
			return
		}
		http.Redirect(w, r, "/boox/cleanup?batch="+url.QueryEscape(token), http.StatusSeeOther)
		return
	}
	if r.Method != "GET" {
		http.NotFound(w, r)
		return
	}
	var d cleanupPage
	var e error
	if id := r.URL.Query().Get("batch"); id != "" {
		d, e = s.cleanupStatus(r.Context(), id)
		if errors.Is(e, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if d.Pending > 0 {
			w.Header().Set("Refresh", "5")
		}
	} else {
		d, e = s.cleanupCandidates(r.Context())
		if e == nil {
			var rows *sql.Rows
			rows, e = s.DB.QueryContext(r.Context(), `SELECT payload->>'cleanupBatch',min(created_at),count(*) FROM boox_operation WHERE payload ? 'cleanupBatch' GROUP BY payload->>'cleanupBatch' ORDER BY min(created_at) DESC LIMIT 10`)
			if e == nil {
				for rows.Next() {
					var v cleanupBatch
					if e = rows.Scan(&v.ID, &v.Created, &v.Total); e != nil {
						break
					}
					d.Recent = append(d.Recent, v)
				}
				if e == nil {
					e = rows.Err()
				}
				rows.Close()
			}
		}
	}
	if e != nil {
		failure(w, e)
		return
	}
	s.renderPage(w, r, cleanupTemplate, "Clean up BOOX notebooks", d)
}

var cleanupTemplate = template.Must(template.New("cleanup").Funcs(template.FuncMap{"ms": listview.Date}).Parse(`{{define "content"}}{{$d:=.Data}}
<p><a href="/boox">← BOOX notebooks</a></p><h1>Clean up numbered notebooks</h1>
{{if $d.Batch}}
<h2>Cleanup progress</h2><p><strong>{{$d.Published}} published</strong> · {{$d.Pending}} pending · {{$d.Blocked}} blocked or superseded · {{$d.Cancelled}} cancelled · {{$d.Total}} total</p>
<p>Published means the deletion and sync notification reached the native server. Devices apply it through their normal sync; it does not confirm that every device has received it. Pending publication retries automatically. This page refreshes while work remains.</p>
<p>Blocked notebooks changed after review or belong to a replaced library. Superseded means a native edit followed publication. <a href="/boox/cleanup">Review current matches</a> before trying again.</p>
{{if $d.Pending}}<form method="post"><input type="hidden" name="token" value="{{$d.Batch}}"><button class="destructive" name="action" value="cancel">Cancel not yet published</button></form><p class="muted">Cancellation stops queued operations only. Publication already in progress may complete.</p>{{end}}
<div class="table-scroll"><table><thead><tr><th>Notebook</th><th>State</th><th>Details</th></tr></thead><tbody>{{range $d.Items}}<tr><td><a href="/boox/notebook?id={{.ID}}">{{.Title}}</a></td><td>{{.State}}</td><td>{{.Detail}}</td></tr>{{end}}</tbody></table></div>
<p><a href="/boox?show=deleted">Deleted notebooks</a> · <a href="/boox/cleanup?batch={{$d.Batch}}">Refresh</a></p>
{{else}}
<p>Find live BOOX notebooks named exactly <code>Notebook-</code> followed by digits, such as <code>Notebook-1</code> or <code>Notebook-023</code>, across all folders. Folders, deleted notebooks and names with extra text are excluded.</p>
<p>This is a name-based cleanup; pages are not inspected for emptiness. Open any notebook to check it, and uncheck anything you want to keep.</p>
<p>Deletion syncs to every BOOX device using this library. Retained history and uploaded assets are preserved; this does not reclaim their storage space.</p>
<p><strong>{{$d.Total}} matching notebooks</strong>{{if gt $d.Total 200}} · showing the first 200. Run another review after these finish to handle the remaining matches.{{end}}</p>
{{if $d.Items}}<form method="post" class="stack library-review"><input type="hidden" name="token" value="{{$d.Token}}">
<div class="table-scroll"><table class="recognition-notebooks"><thead><tr><th>Delete</th><th>Name</th><th>Listed pages</th><th>Created (UTC)</th><th>Modified (UTC)</th></tr></thead><tbody>{{range $d.Items}}<tr><td><input type="checkbox" name="selected" value="{{.Choice}}" checked aria-label="Delete {{.Title}}"></td><td><a href="/boox/notebook?id={{.ID}}" target="_blank" rel="noopener">{{.Title}}</a></td><td>{{.Pages}}{{if .Truncated}}+{{end}}</td><td>{{ms .Created}}</td><td>{{ms .Modified}}</td></tr>{{end}}</tbody></table></div>
<p class="muted">Page counts come from native metadata and can be incomplete. Changed notebooks require a new review. This operation continues if you close the browser.</p>
<label><input type="checkbox" name="confirm" required>Delete the selected notebooks from this library and its synced BOOX devices.</label>
<button name="action" value="queue" class="destructive">Delete selected notebooks</button></form>
{{else}}<p class="empty">No numbered notebooks to clean up.</p>{{end}}
{{if $d.Recent}}<h2>Recent cleanups</h2><ul>{{range $d.Recent}}<li><a href="/boox/cleanup?batch={{.ID}}">{{.Created.UTC.Format "2006-01-02 15:04:05"}} UTC · {{.Total}} notebooks</a></li>{{end}}</ul>{{end}}
{{end}}{{end}}`))
