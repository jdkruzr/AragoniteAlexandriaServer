// boox-audit inspects a private native backup without modifying it. Reports
// contain aggregate format counts only, never titles, text or native identities.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"

	bp "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

type report struct {
	PDFBackgroundsSkipped                                                          int
	Notebooks, Pages, DecodedPages, PagesWithoutDecodedShapesOrText, VisibleShapes int
	BackgroundsChecked                                                             bool
	Shapes, Matrices, Templates, Scenes, Errors, Warnings                          map[string]int
}

func main() {
	a := flag.String("archive", "", "private .note backup")
	o := flag.String("output", "", "aggregate JSON report")
	backgrounds := flag.Bool("backgrounds", true, "also rasterize page backgrounds (can take several minutes)")
	pdf := flag.Bool("pdf", true, "rasterize PDF backgrounds as well")
	flag.Parse()
	if err := run(*a, *o, *backgrounds, *pdf); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func read(f *zip.File) ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("resource unavailable")
	}
	if f.UncompressedSize64 > 32<<20 {
		return nil, fmt.Errorf("resource too large")
	}
	r, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, (32<<20)+1))
}
func run(archive, output string, backgrounds, pdf bool) error {
	if output == "" {
		return fmt.Errorf("explicit output path required")
	}
	z, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer z.Close()
	assets := map[string]map[string]*zip.File{}
	var tree []byte
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		parts := strings.SplitN(f.Name, "/", 3)
		if len(parts) == 2 && parts[1] == "note_tree" {
			tree, e = read(f)
			if e != nil {
				return e
			}
		}
		if len(parts) != 3 {
			continue
		}
		id, key := parts[1], parts[2]
		if strings.HasPrefix(key, "point/") {
			key = "point/" + path.Base(key)
		}
		if assets[id] == nil {
			assets[id] = map[string]*zip.File{}
		}
		assets[id][key] = f
	}
	r := report{BackgroundsChecked: backgrounds, Shapes: map[string]int{}, Matrices: map[string]int{}, Templates: map[string]int{}, Scenes: map[string]int{}, Errors: map[string]int{}, Warnings: map[string]int{}}
	if len(tree) == 0 {
		return fmt.Errorf("backup note_tree missing")
	}
	for len(tree) > 0 {
		n, t, k := protowire.ConsumeTag(tree)
		if k < 0 {
			return fmt.Errorf("invalid note_tree")
		}
		tree = tree[k:]
		if n != 1 || t != protowire.BytesType {
			return fmt.Errorf("unsupported note_tree field")
		}
		b, k := protowire.ConsumeBytes(tree)
		if k < 0 {
			return fmt.Errorf("invalid note record")
		}
		tree = tree[k:]
		var note pb.NoteInfo
		if e = proto.Unmarshal(b, &note); e != nil {
			return e
		}
		if note.Type != 1 || note.Status != 1 {
			continue
		}
		r.Notebooks++
		r.Scenes[note.ActiveScene]++
		m := bp.Metadata{UniqueID: note.UniqueId, PageNameList: json.RawMessage(note.PageNameList)}
		m.Encryption.EncryptionType = int(note.EncryptionType)
		m.ActiveScene, _ = strconv.Atoi(note.ActiveScene)
		if e = json.Unmarshal([]byte(note.NotePageInfo), &m.NotePageInfo); e != nil {
			r.Errors["invalid page metadata"]++
			continue
		}
		a := assets[note.UniqueId]
		keys := []string{}
		for key := range a {
			keys = append(keys, key)
		}
		load := func(key string) ([]byte, error) { return read(a[key]) }
		for _, id := range bp.PageIDs(m.PageNameList) {
			r.Pages++
			// Count all archive records separately from the resolved visible shapes.
			for key, f := range a {
				if !strings.HasPrefix(key, "shape/"+id+"#") {
					continue
				}
				raw, err := read(f)
				if err != nil {
					continue
				}
				zz, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
				if err != nil {
					continue
				}
				for _, sf := range zz.File {
					sb, err := read(sf)
					if err != nil {
						continue
					}
					var sl pb.ShapeInfoProtoList
					if proto.Unmarshal(sb, &sl) != nil {
						continue
					}
					for _, s := range sl.Proto {
						r.Shapes[strconv.Itoa(int(s.ShapeType))]++
						kind := "empty"
						if strings.HasPrefix(s.MatrixValues, "{") {
							kind = "wrapped"
						} else if strings.HasPrefix(s.MatrixValues, "[") {
							kind = "array"
						}
						r.Matrices[kind]++
					}
				}
			}
			raw, err := load("template/json/" + id + ".template_json")
			if err == nil {
				var t struct{ Properties struct{ LayoutType string } }
				if json.Unmarshal(raw, &t) == nil {
					r.Templates[t.Properties.LayoutType]++
				} else {
					r.Templates["legacy-or-invalid"]++
				}
			} else {
				r.Templates["missing"]++
			}
			p, w, err := bp.ShapePage(m, id, keys, load)
			if err != nil {
				r.Errors[err.Error()]++
				continue
			}
			r.DecodedPages++
			r.VisibleShapes += len(p.Shapes)
			if len(p.Shapes) == 0 && len(p.Texts) == 0 {
				r.PagesWithoutDecodedShapesOrText++
			}
			for _, v := range w {
				r.Warnings[v]++
			}
			if backgrounds && !pdf && p.Background != nil && (p.Background.Kind == "pdf" || p.Background.Kind == "import_pdf") {
				r.PDFBackgroundsSkipped++
			} else if backgrounds {
				// Background qualification uses the same compositor; no PNGs are retained.
				_, tw, err := bp.Preview(&bp.Page{PageID: id, Width: p.Width, Height: p.Height, Background: p.Background}, load)
				if err != nil {
					r.Errors[err.Error()]++
				}
				for _, v := range tw {
					r.Warnings[v]++
				}
			}
		}
	}
	f, e := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
