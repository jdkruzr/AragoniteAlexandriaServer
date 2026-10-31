package boox

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image/jpeg"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
)

// notebookPDFPages caps a notebook download; each page is drawn in full.
var notebookPDFPages = 200

// notebookPDF serves a notebook's pages as a PDF of page images, drawn the
// way the reader shows them. It runs in the export slot so a long notebook
// can't starve page previews.
func (s Service) notebookPDF(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	n, e := s.notebookSnapshot(ctx, q.Get("id"))
	if errors.Is(e, sql.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if e != nil {
		s.failure(w, "notebookPDF", e)
		return
	}
	if v := q.Get("version"); v != "" && v != n.Version {
		http.Error(w, "Content changed; reload the notebook and download again.", http.StatusConflict)
		return
	}
	ids := booxpage.PageIDs(n.Meta.PageNameList)
	if len(ids) == 0 {
		http.Error(w, "This notebook has no pages to download.", http.StatusNotFound)
		return
	}
	if len(ids) > notebookPDFPages {
		http.Error(w, fmt.Sprintf("This notebook has %d pages; downloads are limited to %d. Your BOOX can export it as a PDF instead.", len(ids), notebookPDFPages), http.StatusRequestEntityTooLarge)
		return
	}
	wait, cancel := context.WithTimeout(ctx, exportWait)
	defer cancel()
	select {
	case exportSlots <- struct{}{}:
		defer func() { <-exportSlots }()
	case <-wait.Done():
		w.Header().Set("Retry-After", "10")
		http.Error(w, "Another download is still being made. Try again in a moment.", http.StatusServiceUnavailable)
		return
	}
	pdf := fpdf.New("P", "pt", "A4", "")
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle(n.Meta.Title, true)
	for i := range ids {
		if e = ctx.Err(); e != nil {
			return
		}
		_, img, e := s.drawPage(ctx, n, i+1)
		if e != nil {
			http.Error(w, fmt.Sprintf("Page %d couldn't be drawn, so the PDF wasn't made. Open the page to see why.", i+1), http.StatusUnprocessableEntity)
			return
		}
		var b bytes.Buffer
		if e = jpeg.Encode(&b, img, &jpeg.Options{Quality: 85}); e != nil {
			s.failure(w, "notebookPDF", e)
			return
		}
		size := img.Bounds().Size()
		// Pages are drawn at the tablet's resolution, about 220 dpi; 3 px per
		// point prints them near their real size.
		wPt, hPt := float64(size.X)/3, float64(size.Y)/3
		pdf.AddPageFormat("P", fpdf.SizeType{Wd: wPt, Ht: hPt})
		ref := "p" + strconv.Itoa(i)
		opt := fpdf.ImageOptions{ImageType: "JPEG"}
		pdf.RegisterImageOptionsReader(ref, opt, &b)
		pdf.ImageOptions(ref, 0, 0, wPt, hPt, false, opt, 0, "")
	}
	if e = pdf.Error(); e != nil {
		s.failure(w, "notebookPDF", e)
		return
	}
	name := strings.TrimSpace(n.Meta.Title)
	if name == "" {
		name = "Notebook"
	}
	attachment(w, name+".pdf")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-store")
	if e = pdf.Output(w); e != nil {
		slog.Warn("notebook PDF interrupted", "notebook", n.Meta.UniqueID, "error", e)
	}
}
