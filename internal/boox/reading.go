package boox

import (
	"context"
	"encoding/json"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxvisibility"
	listview "github.com/jdkruzr/AragoniteAlexandriaServer/internal/listing"
)

type readingItem struct {
	ID, NativeID, Title, Authors, Quote, Note, Progress, Page, Kind, Status, Cover string
	CreatedAt, ModifiedAt, LastAccess                                              int64
	Stored                                                                         bool
	Highlights, Bookmarks                                                          int
	Children                                                                       []readingItem
}
type readingFolder struct {
	ID, Parent, Name, Path, Revision string
	Writable                         bool
}
type readingCounts struct{ New, Reading, Finished, Highlights, Bookmarks int }
type readingView struct {
	Listing                                     listview.State
	Items                                       []readingItem
	View, Layout, Query, Folder, Status, Record string
	Return                                      string
	Folders, Crumbs                             []readingFolder
	Counts                                      readingCounts
}

func (v readingView) Link(key, value string) string {
	q := url.Values{"view": {v.View}, "layout": {v.Layout}, "sort": {v.Listing.Sort}, "order": {v.Listing.Order}}
	if v.Query != "" {
		q.Set("q", v.Query)
	}
	if v.Folder != "" {
		q.Set("folder", v.Folder)
	}
	if v.Status != "" {
		q.Set("status", v.Status)
	}
	q.Set(key, value)
	if key == "view" {
		q.Del("status")
		q.Del("folder")
	}
	return "/boox/reading?" + q.Encode()
}

var readingTemplate = template.Must(template.New("reading.html").Funcs(listFuncs()).ParseFS(templateFS, "templates/reading.html"))

func nativeString(m map[string]any, key string) string {
	switch v := m[key].(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
func readingFromBody(id string, raw []byte) readingItem {
	var m map[string]any
	_ = nativeJSON(raw, &m)
	v := readingItem{ID: id, NativeID: nativeString(m, "uniqueId"), Title: nativeString(m, "title"), Progress: nativeString(m, "progress"), Page: nativeString(m, "pageNumber"), Quote: nativeString(m, "quote"), Note: nativeString(m, "note")}
	if v.Title == "" {
		v.Title = nativeString(m, "name")
	}
	if v.Title == "" {
		v.Title = "Untitled reading record"
	}
	v.Kind = map[string]string{"1": "Annotation", "2": "Bookmark", "4": "Book"}[nativeString(m, "modeType")]
	v.Status = map[string]string{"0": "New", "1": "In Progress", "2": "Finished"}[nativeString(m, "readingStatus")]
	v.CreatedAt, _ = strconv.ParseInt(nativeString(m, "createdAt"), 10, 64)
	v.ModifiedAt, _ = strconv.ParseInt(nativeString(m, "updatedAt"), 10, 64)
	v.LastAccess, _ = strconv.ParseInt(nativeString(m, "lastAccess"), 10, 64)
	if a, ok := m["authors"].([]any); ok {
		var names []string
		for _, x := range a {
			if s, ok := x.(string); ok {
				names = append(names, s)
			}
		}
		v.Authors = strings.Join(names, ", ")
	} else {
		v.Authors = nativeString(m, "authors")
	}
	return v
}

func (s Service) readingFolders(ctx context.Context) ([]readingFolder, error) {
	return s.readingFoldersFrom(ctx, s.DB)
}
func (s Service) readingFoldersFrom(ctx context.Context, db pg.Querier) ([]readingFolder, error) {
	rows, e := db.QueryContext(ctx, `SELECT body,revision FROM boox_projection WHERE native_uid=$1 AND domain='reading' AND body->>'modeType'='3' AND coalesce(body->>'status','0')='0' AND NOT body ? 'commitType'`, s.uid())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []readingFolder
	for rows.Next() {
		var raw []byte
		var f readingFolder
		if e = rows.Scan(&raw, &f.Revision); e != nil {
			return nil, e
		}
		var m map[string]any
		if e = nativeJSON(raw, &m); e != nil {
			return nil, e
		}
		f.ID = nativeString(m, "uniqueId")
		f.Parent = nativeString(m, "parentUniqueId")
		f.Name = nativeString(m, "name")
		f.Writable = nativeShelf(m)
		out = append(out, f)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	byID := map[string]readingFolder{}
	for _, f := range out {
		byID[f.ID] = f
	}
	for i, f := range out {
		parts := []string{f.Name}
		seen := map[string]bool{f.ID: true}
		parent := f.Parent
		for parent != "" {
			p, ok := byID[parent]
			if !ok || seen[parent] || len(parts) > 32 {
				out[i].Writable = false
				parts = append([]string{"…"}, parts...)
				break
			}
			seen[parent] = true
			parts = append([]string{p.Name}, parts...)
			out[i].Writable = out[i].Writable && p.Writable
			parent = p.Parent
		}
		out[i].Path = strings.Join(parts, " / ")
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].ID < out[j].ID
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func readingPreference(w http.ResponseWriter, r *http.Request, key, def string, allowed ...string) string {
	value := r.URL.Query().Get(key)
	if value == "" {
		if c, e := r.Cookie("alexandria_reading_" + key); e == nil {
			value = c.Value
		}
	}
	valid := false
	for _, a := range allowed {
		valid = valid || value == a
	}
	if !valid {
		value = def
	}
	if r.URL.Query().Has(key) {
		http.SetCookie(w, &http.Cookie{Name: "alexandria_reading_" + key, Value: value, Path: "/boox", MaxAge: 31536000, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || strings.HasPrefix(r.Header.Get("X-Forwarded-Proto"), "https")})
	}
	return value
}

// Aggregate native bindings once, rather than scanning the object catalog for each book.
const readingAssets = `WITH assets AS MATERIALIZED (
 SELECT split_part(l.native_key,'/',3) AS book,
 bool_or(split_part(l.native_key,'/',4)='book' AND v.bytes>0) AS stored,
 max(l.native_key) FILTER(WHERE split_part(l.native_key,'/',4)='thumbnail' AND v.bytes BETWEEN 1 AND 4194304 AND v.content_type IN ('image/png','image/jpeg','image/webp')) AS cover
 FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id
 WHERE starts_with(l.native_key,$1||'/reader/') AND NOT v.deleted GROUP BY 1
) `

func (s Service) reading(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v := readingView{View: readingPreference(w, r, "view", "stored", "stored", "all", "metadata", "stats"), Layout: readingPreference(w, r, "layout", "shelf", "shelf", "list"), Query: q.Get("q"), Folder: q.Get("folder"), Status: q.Get("status"), Record: q.Get("record"), Listing: listview.Parse(q)}
	v.Return = bookReturn(r.URL.RequestURI())
	if len(v.Query) > 200 {
		v.Query = v.Query[:200]
	}
	if v.Status != "0" && v.Status != "1" && v.Status != "2" {
		v.Status = ""
	}
	var e error
	v.Folders, e = s.readingFolders(r.Context())
	if e != nil {
		s.failure(w, "readingFolders", e)
		return
	}
	// Stored books only live on native shelves; cloud-account and smart shelves
	// would always be empty here.
	if v.View == "stored" {
		native := v.Folders[:0]
		for _, f := range v.Folders {
			if f.Writable {
				native = append(native, f)
			}
		}
		v.Folders = native
	}
	parent := v.Folder
	seen := map[string]bool{}
	for parent != "" && parent != "unfiled" && !seen[parent] {
		seen[parent] = true
		found := false
		for _, f := range v.Folders {
			if f.ID == parent {
				v.Crumbs = append([]readingFolder{f}, v.Crumbs...)
				parent = f.Parent
				found = true
				break
			}
		}
		if !found {
			break
		}
	}
	if v.View == "stats" && v.Record == "" {
		e = s.DB.QueryRowContext(r.Context(), `SELECT count(*) FILTER(WHERE body->>'modeType'='4' AND body->>'readingStatus'='0'),count(*) FILTER(WHERE body->>'modeType'='4' AND body->>'readingStatus'='1'),count(*) FILTER(WHERE body->>'modeType'='4' AND body->>'readingStatus'='2'),count(*) FILTER(WHERE body->>'modeType'='1'),count(*) FILTER(WHERE body->>'modeType'='2') FROM boox_projection p WHERE native_uid=$1 AND `+booxvisibility.Reading("p"), s.uid()).Scan(&v.Counts.New, &v.Counts.Reading, &v.Counts.Finished, &v.Counts.Highlights, &v.Counts.Bookmarks)
		if e != nil {
			s.failure(w, "readingCounts", e)
			return
		}
	}
	order := v.Listing.NativeOrder()
	limit := 60
	if v.View == "stats" && v.Record == "" {
		order = `CASE WHEN body->>'lastAccess' ~ '^[0-9]{1,15}$' THEN NULLIF((body->>'lastAccess')::bigint,0) END DESC NULLS LAST,p.document_id`
		limit = 12
		v.Listing.Offset = 0
	}
	const membership = `EXISTS(SELECT 1 FROM boox_projection m JOIN boox_projection f ON f.document_id=m.native_uid||'#'||(m.body->>'libraryUniqueId') WHERE m.native_uid=p.native_uid AND m.domain='reading' AND m.body->>'modeType'='5' AND coalesce(m.body->>'status','0')='0' AND m.body->>'documentUniqueId'=p.body->>'uniqueId' AND f.domain='reading' AND f.native_uid=p.native_uid AND f.body->>'modeType'='3' AND coalesce(f.body->>'status','0')='0' AND ($4='unfiled' OR m.body->>'libraryUniqueId'=$4))`
	rows, e := s.DB.QueryContext(r.Context(), readingAssets+`SELECT p.document_id,p.body,coalesce(a.stored,false),coalesce(a.cover,''),count(*) OVER() FROM boox_projection p LEFT JOIN assets a ON a.book=p.body->>'uniqueId' WHERE p.native_uid=$1 AND `+booxvisibility.Reading("p")+` AND ($2<>'' AND p.document_id=$2 OR $2='' AND p.body->>'modeType'='4' AND ($3 NOT IN ('stored','metadata') OR $3='stored' AND coalesce(a.stored,false) OR $3='metadata' AND NOT coalesce(a.stored,false)) AND ($4='' OR $4='unfiled' AND NOT `+membership+` OR $4<>'unfiled' AND `+membership+`) AND ($5='' OR strpos(lower(coalesce(p.body->>'title',p.body->>'name','')||' '||coalesce(p.body->>'authors','')),lower($5))>0) AND ($6='' OR p.body->>'readingStatus'=$6) AND ($3<>'stats' OR (p.body->>'lastAccess' ~ '^[0-9]{1,15}$' AND p.body->>'lastAccess'<>'0'))) ORDER BY `+order+` LIMIT $7 OFFSET $8`, s.uid(), v.Record, v.View, v.Folder, v.Query, v.Status, limit, v.Listing.Offset)
	if e != nil {
		s.failure(w, "reading", e)
		return
	}
	for rows.Next() {
		var raw []byte
		var id, cover string
		var stored bool
		if e = rows.Scan(&id, &raw, &stored, &cover, &v.Listing.Total); e != nil {
			break
		}
		item := readingFromBody(id, raw)
		item.Stored = stored
		if cover != "" {
			item.Cover = "/boox/books/cover?record=" + url.QueryEscape(id)
		}
		v.Items = append(v.Items, item)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		s.failure(w, "reading", e)
		return
	}
	if v.Record != "" && len(v.Items) == 1 && v.Items[0].Kind == "Book" {
		rows, e = s.DB.QueryContext(r.Context(), `SELECT document_id,body FROM boox_projection p WHERE native_uid=$1 AND body->>'documentId'=$2 AND body->>'modeType' IN ('1','2') AND `+booxvisibility.Reading("p")+` ORDER BY `+listview.State{Sort: "created", Order: "asc"}.NativeOrder(), s.uid(), v.Items[0].NativeID)
		if e != nil {
			s.failure(w, "readingDetails", e)
			return
		}
		for rows.Next() {
			var id string
			var raw []byte
			if e = rows.Scan(&id, &raw); e != nil {
				break
			}
			v.Items[0].Children = append(v.Items[0].Children, readingFromBody(id, raw))
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			s.failure(w, "readingDetails", e)
			return
		}
	}
	if len(v.Items) == 0 && v.Listing.Offset > 0 {
		q.Del("offset")
		http.Redirect(w, r, "/boox/reading?"+q.Encode(), 303)
		return
	}
	q.Set("view", v.View)
	q.Set("layout", v.Layout)
	v.Listing.Finish(q, "/boox/reading", v.Listing.Total)
	s.renderPage(w, r, readingTemplate, "BOOX Reading", v)
}
func (s Service) readingCover(w http.ResponseWriter, r *http.Request) {
	var key string
	e := s.DB.QueryRowContext(r.Context(), readingAssets+`SELECT a.cover FROM boox_projection p JOIN assets a ON a.book=p.body->>'uniqueId' WHERE p.document_id=$2 AND p.native_uid=$1 AND p.body->>'modeType'='4' AND a.cover IS NOT NULL AND `+booxvisibility.Reading("p"), s.uid(), r.URL.Query().Get("record")).Scan(&key)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	var sha, ct string
	var size int64
	e = s.DB.QueryRowContext(r.Context(), `SELECT v.sha256,v.content_type,v.bytes FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE l.native_key=$1 AND NOT v.deleted`, key).Scan(&sha, &ct, &size)
	if e != nil || size > 4<<20 {
		http.NotFound(w, r)
		return
	}
	body, _, e := s.Objects.Get(r.Context(), "boox/bodies/"+sha)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	defer body.Close()
	raw, e := io.ReadAll(io.LimitReader(body, 4<<20+1))
	if e != nil || int64(len(raw)) != size || hash(raw) != sha {
		http.NotFound(w, r)
		return
	}
	detected := http.DetectContentType(raw)
	if detected != "image/png" && detected != "image/jpeg" && detected != "image/webp" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", detected)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Write(raw)
}
