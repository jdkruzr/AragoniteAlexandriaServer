// boox-preview is an offline qualification utility. Inputs may contain private
// notes; output paths are explicit and no content is printed to stdout.
package main

import (
	"archive/zip"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage"
	"image/png"
	"io"
	"os"
	"path"
	"strings"
)

func main() {
	archive := flag.String("archive", "", "private .note archive")
	metadata := flag.String("metadata", "", "native metadata JSON")
	output := flag.String("output", "", "PNG output")
	page := flag.Int("page", 1, "one-based page")
	flag.Parse()
	if e := run(*archive, *metadata, *output, *page); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run(archive, metadata, output string, page int) error {
	raw, e := os.ReadFile(metadata)
	if e != nil {
		return e
	}
	var m booxpage.Metadata
	if e = json.Unmarshal(raw, &m); e != nil {
		return e
	}
	z, e := zip.OpenReader(archive)
	if e != nil {
		return e
	}
	defer z.Close()
	assets := map[string]*zip.File{}
	var keys []string
	for _, f := range z.File {
		if f.FileInfo().IsDir() {
			continue
		}
		needle := "/" + m.UniqueID + "/"
		i := strings.Index("/"+f.Name, needle)
		if i < 0 {
			continue
		}
		key := ("/" + f.Name)[i+len(needle):]
		if strings.HasPrefix(key, "point/") {
			key = "point/" + path.Base(key)
		}
		assets[key] = f
		keys = append(keys, key)
	}
	load := func(key string) ([]byte, error) {
		f := assets[key]
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
		return io.ReadAll(io.LimitReader(r, 32<<20))
	}
	m, e = booxpage.ResolvePages(m, keys, load)
	if e != nil {
		return e
	}
	ids := booxpage.PageIDs(m.PageNameList)
	if page < 1 || page > len(ids) {
		return fmt.Errorf("page outside notebook")
	}
	p, w, e := booxpage.ShapePage(m, ids[page-1], keys, load)
	if e != nil {
		return e
	}
	img, tw, e := booxpage.Preview(p, load)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if e = png.Encode(f, img); e != nil {
		return e
	}
	fmt.Printf("Rendered %d shapes at %.0fx%.0f; warnings: %v\n", len(p.Shapes), p.Width, p.Height, append(w, tw...))
	return nil
}
