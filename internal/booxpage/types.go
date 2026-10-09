// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

// Page represents a single page within a note.
type Page struct {
	PageID     string
	Width      float64
	Height     float64
	OrderIndex float32 // from VirtualPage protobuf; used for sorting
	Texts      []string
	Shapes     []*Shape // ordered by zorder
}

// Shape represents a single shape (stroke, geometry, text, etc.) on a page.
type Shape struct {
	UniqueID     string
	ShapeType    int32
	Color        int32 // ARGB packed
	FillColor    int32 // ARGB packed
	Thickness    float32
	ZOrder       int32
	BoundingRect *Rect
	MatrixValues []float64
	Text         string
	ImagePath    string
	RevisionID   string
	Points       []TinyPoint // populated from point files or inline pointList
}

// Rect is a bounding rectangle parsed from JSON.
type Rect struct {
	Left   float64 `json:"left"`
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
}

// TinyPoint is a single stroke sample (16 bytes in the binary format).
type TinyPoint struct {
	X        float32
	Y        float32
	Size     int16
	Pressure int16
	Time     uint32
}
