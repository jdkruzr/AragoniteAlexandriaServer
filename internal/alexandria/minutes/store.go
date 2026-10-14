package minutes

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/pg"
)

// Source is a recording chosen when minutes were requested.
type Source struct {
	Key        string    `json:"key"`
	SHA256     string    `json:"sha256"`
	Bytes      int64     `json:"bytes"`
	Title      string    `json:"title"`
	RecordedAt time.Time `json:"recorded_at,omitzero"`
	DurationMS int64     `json:"duration_ms"`
}

// Transcript is a recording with its transcription state.
type Transcript struct {
	Source
	State, Detail, Language, Model, Text string
	Segments                             []Segment
}

func (t Transcript) Duration() time.Duration { return time.Duration(t.DurationMS) * time.Millisecond }

// Metadata describes the inputs the minutes were written from.
type Metadata struct {
	Title         string             `json:"title"`
	Folder        string             `json:"folder,omitempty"`
	Created       time.Time          `json:"created,omitzero"`
	Modified      time.Time          `json:"modified,omitzero"`
	PageCount     int                `json:"page_count"`
	PagesWithText int                `json:"pages_with_text"`
	Missing       []int              `json:"missing_pages,omitempty"`
	Recordings    []RecordingSummary `json:"recordings"`
	Summarizer    string             `json:"summary_model"`
	Transcriber   string             `json:"transcription_model,omitempty"`
}

type RecordingSummary struct {
	Title      string    `json:"title"`
	RecordedAt time.Time `json:"recorded_at,omitzero"`
	DurationMS int64     `json:"duration_ms"`
	Language   string    `json:"language,omitempty"`
	Words      int       `json:"words"`
}

type Stored struct {
	Source, NotebookID, RequestID, State, Detail, Model, InputHash string
	Minutes
	Metadata    Metadata
	Recordings  []Transcript
	RequestedAt time.Time
	FinishedAt  sql.NullTime
	Updated     time.Time
}

// Total recording time of the stored sources.
func (s Stored) Length() time.Duration {
	var d time.Duration
	for _, r := range s.Recordings {
		d += r.Duration()
	}
	return d
}

// Load returns the notebook's current minutes and their transcripts.
func Load(ctx context.Context, db pg.DB, source, id string) (Stored, bool, error) {
	out := Stored{Source: source, NotebookID: id}
	var recs, points, meta, keywords []byte
	err := db.QueryRowContext(ctx, `SELECT request_id::text,state,detail,model,input_hash,topic,keywords,key_points,conclusion,metadata,recordings,requested_at,finished_at,updated_at FROM alexandria_note_minutes WHERE source=$1 AND notebook_id=$2`, source, id).
		Scan(&out.RequestID, &out.State, &out.Detail, &out.Model, &out.InputHash, &out.Topic, &keywords, &points, &out.Conclusion, &meta, &recs, &out.RequestedAt, &out.FinishedAt, &out.Updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	var sources []Source
	if json.Unmarshal(keywords, &out.Keywords) != nil || json.Unmarshal(recs, &sources) != nil || json.Unmarshal(points, &out.KeyPoints) != nil || json.Unmarshal(meta, &out.Metadata) != nil {
		return out, false, errors.New("stored minutes are unreadable")
	}
	for _, s := range sources {
		t := Transcript{Source: s, State: "missing"}
		var segs []byte
		err = db.QueryRowContext(ctx, `SELECT state,detail,language,model,text,segments,CASE WHEN duration_ms>0 THEN duration_ms ELSE $5 END FROM alexandria_transcription WHERE source=$1 AND notebook_id=$2 AND resource_key=$3 AND sha256=$4`, source, id, s.Key, s.SHA256, s.DurationMS).
			Scan(&t.State, &t.Detail, &t.Language, &t.Model, &t.Text, &segs, &t.DurationMS)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, false, err
		}
		if len(segs) > 0 {
			_ = json.Unmarshal(segs, &t.Segments)
		}
		out.Recordings = append(out.Recordings, t)
	}
	return out, true, nil
}
