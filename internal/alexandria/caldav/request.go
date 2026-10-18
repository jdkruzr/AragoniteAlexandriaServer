package caldav

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/http"

	gocaldav "github.com/emersion/go-webdav/caldav"
)

const maxXMLBody = 8 << 20

// readXMLBody owns error responses so every wrapper handles limits identically.
// Empty PROPFIND bodies are valid; nonempty bodies must contain one XML root.
func readXMLBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxXMLBody))
	if err != nil {
		var limit *http.MaxBytesError
		code := http.StatusBadRequest
		if errors.As(err, &limit) {
			code = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(code), code)
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 && r.Method == "PROPFIND" {
		return raw, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	depth, roots := 0, 0
	for {
		tok, e := decoder.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			err = e
			break
		}
		switch v := tok.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			if depth == 0 && len(bytes.TrimSpace(v)) != 0 {
				err = errors.New("text outside XML root")
			}
		}
		if err != nil {
			break
		}
	}
	if err == nil && (depth != 0 || roots != 1) {
		err = errors.New("expected one XML root")
	}
	if err != nil {
		http.Error(w, "invalid XML body", http.StatusBadRequest)
		return nil, err
	}
	return raw, nil
}
func filterCalendarObjects(all []gocaldav.CalendarObject, query *gocaldav.CalendarQuery, err error) ([]gocaldav.CalendarObject, error) {
	if err != nil || query == nil || query.CompFilter.Name == "" {
		return all, err
	}
	return gocaldav.Filter(query, all)
}
