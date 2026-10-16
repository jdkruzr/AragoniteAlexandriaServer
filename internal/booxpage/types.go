// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

// Page represents a single page within a note.
type Page struct {
	Notices    []string
	Background *Background
	PageID     string
	Width      float64
	Height     float64
	OrderIndex float32 // from VirtualPage protobuf; used for sorting
	Texts      []string
	Audio      []AudioRef // recordings placed on this page
	Shapes     []*Shape   // ordered by zorder
	Issues     []Issue    // shapes that raised a coverage warning
}

// Issue names a shape the preview could not reproduce. Summary describes
// its structure only: field and attribute names, never text or ink.
type Issue struct {
	ShapeID   string
	ShapeType int32
	Warning   string
	Summary   ShapeSummary
}

type ShapeSummary struct {
	CreatedAt, UpdatedAt                 int64
	Width, Height                        float64
	Fields                               []string // non-empty proto fields
	ExtraKeys, ResourceKeys, OptionKeys  []string // top-level JSON keys
	ConnectionKeys, CreateArgKeys        []string
	ResourceExtension                    string
	TextRunes, RichTextBytes, PointBytes int
	TextStyle                            map[string]any `json:",omitempty"` // text boxes only: numbers and flags
}

// Shape represents a single shape (stroke, geometry, text, etc.) on a page.
type Shape struct {
	TextStyle    textStyle
	Features     []featurePath
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
	UnderContent bool        // a marker drawn beneath the writing (darken blend)
	// Recovered strokes came from point data whose shape archive was empty;
	// their pen (and so color) is a guess.
	Recovered bool
	// CreatedAt and UpdatedAt are the firmware's epoch milliseconds (0 = unknown).
	CreatedAt, UpdatedAt int64
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
