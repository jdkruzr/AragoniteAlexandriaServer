package booxpage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/fogleman/gg"
	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
	"google.golang.org/protobuf/proto"
)

type Background struct {
	Kind, Path string
	Index      int
}

// Virtual page metadata binds native page identity to its PDF or template.
// It is a list protobuf, not a single VirtualPage; select by identity/version.
func backgroundFor(meta Metadata, id string, keys []string, load Load) (*Background, error) {
	winner := meta.VirtualPages[id]
	for _, key := range keys {
		if meta.VirtualPages != nil {
			break
		}
		if !strings.HasPrefix(key, "virtual/page/pb/") {
			continue
		}
		raw, e := load(key)
		if e != nil {
			return nil, fmt.Errorf("virtual page metadata unavailable")
		}
		var list pb.VirtualPageList
		if proto.Unmarshal(raw, &list) != nil {
			return nil, fmt.Errorf("invalid virtual page metadata")
		}
		for _, v := range list.Proto {
			if v.PageId != id {
				continue
			}
			if winner == nil || v.UpdatedAt > winner.UpdatedAt {
				winner = v
			} else if v.UpdatedAt == winner.UpdatedAt && !proto.Equal(v, winner) {
				return nil, fmt.Errorf("ambiguous virtual page versions")
			}
		}
	}
	if winner == nil {
		return nil, nil
	}
	v := winner
	p := strings.TrimPrefix(v.ContentRelativePath, "document/"+meta.UniqueID+"/")
	if p != "" && (path.Clean(p) != p || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "../") || strings.Contains(p, "\\")) {
		return nil, fmt.Errorf("invalid page background reference")
	}
	bg := &Background{Kind: v.ContentType, Path: p}
	if v.ContentType == "pdf" || v.ContentType == "import_pdf" {
		var ref struct{ PageIndex *int }
		if json.Unmarshal([]byte(v.ContentPageId), &ref) != nil || ref.PageIndex == nil || *ref.PageIndex < 0 || *ref.PageIndex > 100000 {
			return nil, fmt.Errorf("invalid PDF page reference")
		}
		bg.Index = *ref.PageIndex
	}
	return bg, nil
}
func drawBackground(dc *gg.Context, p *Page, load Load) error {
	if p.Background != nil && (p.Background.Kind == "pdf" || p.Background.Kind == "import_pdf") {
		raw, e := load(p.Background.Path)
		if e != nil {
			return fmt.Errorf("The page PDF has not arrived or failed verification.")
		}
		return drawPDF(dc, raw, p.Background.Index)
	}
	key := "template/json/" + p.PageID + ".template_json"
	if p.Background != nil {
		if p.Background.Kind != "" && p.Background.Kind != "geo_layout" {
			return fmt.Errorf("This page background type is not supported yet.")
		}
		if p.Background.Path != "" {
			key = p.Background.Path
		}
	}
	raw, e := load(key)
	if e != nil {
		return fmt.Errorf("Page template is unavailable; the background may be missing.")
	}
	return drawTemplate(dc, raw, load)
}

// Poppler is already part of the server image. Inputs and outputs remain private
// disposable scratch; fixed argv, page/pixel bounds and a deadline contain work.
func drawPDF(dc *gg.Context, raw []byte, index int) error {
	if len(raw) > 32<<20 || !bytes.HasPrefix(raw, []byte("%PDF-")) {
		return fmt.Errorf("PDF background is invalid or exceeds preview limits.")
	}
	dir, e := os.MkdirTemp("", "alexandria-boox-pdf-")
	if e != nil {
		return fmt.Errorf("PDF scratch unavailable.")
	}
	defer os.RemoveAll(dir)
	input, output := dir+"/input.pdf", dir+"/page"
	if e = os.WriteFile(input, raw, 0600); e != nil {
		return fmt.Errorf("PDF scratch unavailable.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page := strconv.Itoa(index + 1)
	cmd := exec.CommandContext(ctx, "pdftoppm", "-f", page, "-l", page, "-singlefile", "-scale-to-x", strconv.Itoa(dc.Width()), "-scale-to-y", strconv.Itoa(dc.Height()), "-png", input, output)
	if cmd.Run() != nil {
		return fmt.Errorf("PDF page could not be rendered within preview limits.")
	}
	f, e := os.Open(output + ".png")
	if e != nil {
		return fmt.Errorf("PDF page unavailable.")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (32<<20)+1))
	if e != nil || len(b) > 32<<20 {
		return fmt.Errorf("PDF preview exceeds limits.")
	}
	c, _, e := image.DecodeConfig(bytes.NewReader(b))
	if e != nil || c.Width != dc.Width() || c.Height != dc.Height() {
		return fmt.Errorf("PDF preview dimensions invalid.")
	}
	img, _, e := image.Decode(bytes.NewReader(b))
	if e != nil {
		return fmt.Errorf("PDF preview unreadable.")
	}
	dc.DrawImage(img, 0, 0)
	return nil
}
