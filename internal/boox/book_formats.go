package boox

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"mime"
	"net/url"
	"path"
	"sort"
	"strings"
	"unicode"
)

// bookFormats maps each upload extension to the Content-Type its bytes are
// stored with. They are the files the tablet Library lists as books
// (BookFileType.defaultFileTypes), without the store-bound ones. A tablet's
// record type is the lower-cased extension, so a book's kind is too.
// Formats a browser would run as a page are stored as plain bytes.
var bookFormats = map[string]string{
	".pdf":   "application/pdf",
	".epub":  "application/epub+zip",
	".mobi":  "application/x-mobipocket-ebook",
	".azw3":  "application/x-mobi8-ebook",
	".azw":   "application/vnd.amazon.ebook",
	".prc":   "application/x-mobipocket-ebook",
	".pdb":   "application/vnd.palm",
	".cbz":   "application/x-cbz",
	".cbr":   "application/x-cbr",
	".djvu":  "image/vnd.djvu",
	".fb2":   "application/octet-stream",
	".fbz":   "application/x-zip-compressed-fb2",
	".fb3":   "application/fb3",
	".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".pptx":  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
	".ppsx":  "application/vnd.openxmlformats-officedocument.presentationml.slideshow",
	".potx":  "application/vnd.openxmlformats-officedocument.presentationml.template",
	".pptm":  "application/vnd.ms-powerpoint.presentation.macroEnabled.12",
	".ppsm":  "application/vnd.ms-powerpoint.slideshow.macroEnabled.12",
	".potm":  "application/vnd.ms-powerpoint.template.macroEnabled.12",
	".ppam":  "application/vnd.ms-powerpoint.addin.macroEnabled.12",
	".odt":   "application/vnd.oasis.opendocument.text",
	".sxw":   "application/vnd.sun.xml.writer",
	".doc":   "application/msword",
	".ppt":   "application/vnd.ms-powerpoint",
	".pps":   "application/vnd.ms-powerpoint",
	".pot":   "application/vnd.ms-powerpoint",
	".ppa":   "application/vnd.ms-powerpoint",
	".rtf":   "application/rtf",
	".txt":   "text/plain",
	".md":    "text/markdown",
	".html":  "application/octet-stream",
	".htm":   "application/octet-stream",
	".xhtml": "application/octet-stream",
	".xml":   "application/octet-stream",
	".chm":   "application/vnd.ms-htmlhelp",
	".zip":   "application/zip",
}

const errBookFormat = "This file type can't be uploaded."
const errBookFile = "Choose a supported book or document up to 128 MiB with a plain filename."

func bookContentType(kind string) string { return bookFormats["."+kind] }

// bookExtensions lists the upload extensions in order, for the page.
func bookExtensions() []string {
	out := make([]string, 0, len(bookFormats))
	for ext := range bookFormats {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}

// bookServeType is the Content-Type a stored book is sent to a browser or
// file manager with. Tablets upload books with a type of their own choosing,
// so anything a browser would open as a page is sent as plain bytes.
func bookServeType(stored, name string) string {
	ext := strings.ToLower(path.Ext(name))
	ct := stored
	if known, ok := bookFormats[ext]; ok && (ct == "" || known == "application/octet-stream") {
		ct = known
	}
	if ct == "" {
		ct = mime.TypeByExtension(ext)
	}
	media, _, e := mime.ParseMediaType(ct)
	if e != nil || media == "text/html" || media == "text/xml" || media == "application/xml" || strings.HasSuffix(media, "+xml") {
		return "application/octet-stream"
	}
	return ct
}

// validMobi checks a PalmDB "BOOKMOBI" container (MOBI, PRC, AZW and AZW3/KF8
// alike): a record table, the MOBI header in record 0 and no encryption.
func validMobi(raw []byte) error {
	invalid := errors.New("The file is not a valid MOBI or Kindle book.")
	if len(raw) < 86 || string(raw[60:68]) != "BOOKMOBI" {
		return invalid
	}
	records := int(binary.BigEndian.Uint16(raw[76:78]))
	if records < 1 || 78+8*records > len(raw) {
		return invalid
	}
	first := int(binary.BigEndian.Uint32(raw[78:82]))
	if first < 78+8*records || first+32 > len(raw) || string(raw[first+16:first+20]) != "MOBI" {
		return invalid
	}
	if binary.BigEndian.Uint16(raw[first+12:first+14]) != 0 {
		return errors.New("This book is DRM-protected, so BOOX cannot open it.")
	}
	return nil
}

// validPalm checks a Palm database: a record table that fits and a printable
// type and creator. One holding a MOBI book is checked as a MOBI.
func validPalm(raw []byte) error {
	if len(raw) >= 68 && string(raw[60:68]) == "BOOKMOBI" {
		return validMobi(raw)
	}
	invalid := errors.New("The file is not a valid Palm database.")
	if len(raw) < 78 {
		return invalid
	}
	for _, c := range raw[60:68] {
		if c < 0x20 || c > 0x7e {
			return invalid
		}
	}
	if records := int(binary.BigEndian.Uint16(raw[76:78])); records < 1 || 78+8*records > len(raw) {
		return invalid
	}
	return nil
}

// safeZip opens an archive and refuses the shapes that make one unsafe to
// unpack: too many entries, duplicate or escaping paths, and contents that
// expand beyond the limits. label names the format in the refusal.
func safeZip(raw []byte, label string) (map[string]*zip.File, error) {
	z, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil || len(z.File) > 20000 {
		return nil, errors.New("The " + label + " archive is invalid.")
	}
	files := map[string]*zip.File{}
	var total uint64
	for _, f := range z.File {
		if _, exists := files[f.Name]; exists {
			return nil, errors.New("The " + label + " has duplicate archive paths.")
		}
		if strings.HasPrefix(f.Name, "/") || strings.Contains(f.Name, "\\") || path.Clean(f.Name) == ".." || strings.HasPrefix(path.Clean(f.Name), "../") {
			return nil, errors.New("The " + label + " contains an invalid archive path.")
		}
		if f.UncompressedSize64 > 256<<20 || total+f.UncompressedSize64 > 512<<20 {
			return nil, errors.New("The " + label + " expands beyond supported limits.")
		}
		total += f.UncompressedSize64
		files[f.Name] = f
	}
	return files, nil
}

func zipEntry(files map[string]*zip.File, name string, limit int64) ([]byte, error) {
	f := files[name]
	if f == nil || f.UncompressedSize64 > uint64(limit) {
		return nil, errors.New("missing or oversized archive entry")
	}
	r, e := f.Open()
	if e != nil {
		return nil, e
	}
	defer r.Close()
	return io.ReadAll(io.LimitReader(r, limit+1))
}

func epubMimetype(files map[string]*zip.File) bool {
	mime, e := zipEntry(files, "mimetype", 64)
	// Some publishers end the mimetype file with a line break; NeoReader accepts it.
	return e == nil && strings.TrimSpace(string(mime)) == "application/epub+zip"
}

func validEPUB(raw []byte) error {
	files, e := safeZip(raw, "EPUB")
	if e != nil {
		return e
	}
	if !epubMimetype(files) {
		return errors.New("The archive is not an EPUB.")
	}
	container, e := zipEntry(files, "META-INF/container.xml", 1<<20)
	var declaration struct {
		Roots []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if e != nil || xml.Unmarshal(container, &declaration) != nil || len(declaration.Roots) == 0 {
		return errors.New("The EPUB has no package declaration.")
	}
	pkg, e := zipEntry(files, declaration.Roots[0].Path, 4<<20)
	var opf struct {
		XMLName  xml.Name `xml:"package"`
		Manifest []struct {
			ID   string `xml:"id,attr"`
			Href string `xml:"href,attr"`
		} `xml:"manifest>item"`
		Spine []struct {
			ID string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
	}
	if e != nil || xml.Unmarshal(pkg, &opf) != nil || len(opf.Spine) == 0 {
		return errors.New("The EPUB package or reading order is invalid.")
	}
	items := map[string]string{}
	for _, item := range opf.Manifest {
		items[item.ID] = item.Href
	}
	for _, item := range opf.Spine {
		href, ok := items[item.ID]
		u, err := url.Parse(href)
		if !ok || err != nil || u.IsAbs() || u.Host != "" || files[path.Join(path.Dir(declaration.Roots[0].Path), u.Path)] == nil {
			return errors.New("An EPUB reading-order resource is missing.")
		}
	}
	return nil
}

// isRAR reports a RAR 4 or RAR 5 archive by its signature.
func isRAR(raw []byte) bool {
	return bytes.HasPrefix(raw, []byte("Rar!\x1a\x07\x00")) || bytes.HasPrefix(raw, []byte("Rar!\x1a\x07\x01\x00"))
}

// validComicZip checks a zip of page images, the types NeoReader's comic
// reader shows (FileUtils.isImageFile).
func validComicZip(raw []byte, label string) error {
	files, e := safeZip(raw, label)
	if e != nil {
		return e
	}
	if epubMimetype(files) {
		return errors.New("This archive is an EPUB. Rename it to .epub.")
	}
	for name := range files {
		switch strings.ToLower(path.Ext(name)) {
		case ".bmp", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg":
			return nil
		}
	}
	return errors.New("The comic archive has no page images.")
}

// zipWith checks a safe archive that holds a named entry, or, when the name
// starts with a dot, any entry with that extension.
func zipWith(raw []byte, label, entry string) error {
	files, e := safeZip(raw, label)
	if e != nil {
		return e
	}
	if files[entry] != nil {
		return nil
	}
	if strings.HasPrefix(entry, ".") {
		for name := range files {
			if strings.EqualFold(path.Ext(name), entry) {
				return nil
			}
		}
	}
	return errors.New("The file is not a valid " + label + " file.")
}

// xmlRoot is the name of an XML document's root element. Only the bytes
// before it are read, so the document's own encoding is passed through.
func xmlRoot(raw []byte) string {
	d := xml.NewDecoder(bytes.NewReader(raw))
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	for {
		t, e := d.Token()
		if e != nil {
			return ""
		}
		if s, ok := t.(xml.StartElement); ok {
			return s.Name.Local
		}
	}
}

// plainText accepts UTF-16 with a byte-order mark, or bytes with no NUL near
// the start.
func plainText(raw []byte) bool {
	if bytes.HasPrefix(raw, []byte{0xff, 0xfe}) || bytes.HasPrefix(raw, []byte{0xfe, 0xff}) {
		return true
	}
	return bytes.IndexByte(raw[:min(len(raw), 8192)], 0) < 0
}

// validateBook checks a file by its extension and returns its kind.
func validateBook(name string, raw []byte) (string, error) {
	if len(raw) == 0 || len(raw) > maxBook || len(name) > 240 || strings.ContainsAny(name, "/\\") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New(errBookFile)
	}
	ext := strings.ToLower(path.Ext(name))
	if _, ok := bookFormats[ext]; !ok {
		return "", errors.New(errBookFormat)
	}
	kind := ext[1:]
	label := strings.ToUpper(kind)
	invalid := errors.New("The file is not a valid " + label + " file.")
	var e error
	switch kind {
	case "pdf":
		if !bytes.HasPrefix(raw, []byte("%PDF-")) || !bytes.Contains(raw[max(0, len(raw)-1024):], []byte("%%EOF")) {
			e = errors.New("The file does not have a valid PDF header and trailer.")
		}
	case "epub":
		e = validEPUB(raw)
	case "mobi", "azw3", "azw", "prc":
		e = validMobi(raw)
	case "pdb":
		e = validPalm(raw)
	case "cbz":
		// NeoReader picks the unpacker for a .cbz by its name, so a RAR inside fails.
		if isRAR(raw) {
			e = errors.New("This file is a RAR archive. Rename it to .cbr.")
		} else {
			e = validComicZip(raw, label)
		}
	case "cbr":
		// A zip under a .cbr name opens: NeoReader checks the zip signature first.
		if !isRAR(raw) {
			if !bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
				e = invalid
			} else {
				e = validComicZip(raw, label)
			}
		}
	case "djvu":
		if len(raw) < 16 || string(raw[:8]) != "AT&TFORM" || string(raw[12:16]) != "DJVU" && string(raw[12:16]) != "DJVM" {
			e = invalid
		}
	case "fb2":
		if xmlRoot(raw) != "FictionBook" {
			e = invalid
		}
	case "fbz":
		e = zipWith(raw, label, ".fb2")
	case "fb3", "docx", "pptx", "ppsx", "potx", "pptm", "ppsm", "potm", "ppam":
		e = zipWith(raw, label, "[Content_Types].xml")
	case "odt", "sxw":
		e = zipWith(raw, label, "content.xml")
	case "doc", "ppt", "pps", "pot", "ppa":
		if !bytes.HasPrefix(raw, []byte{0xd0, 0xcf, 0x11, 0xe0, 0xa1, 0xb1, 0x1a, 0xe1}) {
			e = invalid
		}
	case "rtf":
		if !bytes.HasPrefix(raw, []byte(`{\rtf`)) {
			e = invalid
		}
	case "chm":
		if !bytes.HasPrefix(raw, []byte("ITSF")) {
			e = invalid
		}
	case "zip":
		var files map[string]*zip.File
		if files, e = safeZip(raw, label); e == nil && len(files) == 0 {
			e = invalid
		}
	default: // txt, md, html, htm, xhtml, xml
		if !plainText(raw) {
			e = invalid
		}
	}
	if e != nil {
		return "", e
	}
	return kind, nil
}
