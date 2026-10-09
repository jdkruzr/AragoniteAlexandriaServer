// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
)

const (
	pointHeaderSize = 76
	xrefEntrySize   = 44
	tinyPointSize   = 16
	pointAttrSize   = 4
)

// parsePointFile reads a V1 point file and returns a map of shapeID → []TinyPoint.
func parsePointFile(data []byte) (map[string][]TinyPoint, error) {
	if len(data) < pointHeaderSize+4 {
		return nil, fmt.Errorf("point file too short: %d bytes", len(data))
	}

	// Read xref offset from last 4 bytes.
	xrefOff := int(binary.BigEndian.Uint32(data[len(data)-4:]))
	if xrefOff < pointHeaderSize || xrefOff >= len(data)-4 {
		return nil, fmt.Errorf("invalid xref offset: %d (file size %d)", xrefOff, len(data))
	}

	// Validate this looks like V1 format by checking header contains
	// a reasonable xref offset and the file is large enough.
	xrefData := data[xrefOff : len(data)-4]
	if len(xrefData)%xrefEntrySize != 0 {
		return nil, fmt.Errorf("booxnote: unsupported point file format version")
	}

	nEntries := len(xrefData) / xrefEntrySize
	result := make(map[string][]TinyPoint, nEntries)

	for i := 0; i < nEntries; i++ {
		entry := xrefData[i*xrefEntrySize : (i+1)*xrefEntrySize]

		// Shape ID: 36 bytes UTF-8, null-trimmed.
		shapeID := strings.TrimRight(string(entry[:36]), "\x00 ")
		offset := int(binary.BigEndian.Uint32(entry[36:40]))
		length := int(binary.BigEndian.Uint32(entry[40:44]))

		if offset < pointHeaderSize || length < pointAttrSize || offset > xrefOff || length > xrefOff-offset || (length-pointAttrSize)%tinyPointSize != 0 {
			return nil, fmt.Errorf("point data block out of range for shape %s", shapeID)
		}

		block := data[offset : offset+length]
		points := decodeTinyPoints(block)
		for _, p := range points {
			if math.IsNaN(float64(p.X)) || math.IsNaN(float64(p.Y)) || math.IsInf(float64(p.X), 0) || math.IsInf(float64(p.Y), 0) || math.Abs(float64(p.X)) > 1e7 || math.Abs(float64(p.Y)) > 1e7 {
				return nil, fmt.Errorf("invalid point coordinate")
			}
		}
		result[shapeID] = points
	}

	return result, nil
}

// decodeTinyPoints decodes a point data block into TinyPoint slice.
// Block format: 4-byte attrs (attrA: int16, attrB: int16) + N × 16-byte points.
func decodeTinyPoints(block []byte) []TinyPoint {
	if len(block) < pointAttrSize {
		return nil
	}
	pointData := block[pointAttrSize:]
	n := len(pointData) / tinyPointSize
	if n == 0 {
		return nil
	}

	points := make([]TinyPoint, n)
	for i := 0; i < n; i++ {
		off := i * tinyPointSize
		rec := pointData[off : off+tinyPointSize]
		points[i] = TinyPoint{
			X:        math.Float32frombits(binary.BigEndian.Uint32(rec[0:4])),
			Y:        math.Float32frombits(binary.BigEndian.Uint32(rec[4:8])),
			Size:     int16(binary.BigEndian.Uint16(rec[8:10])),
			Pressure: int16(binary.BigEndian.Uint16(rec[10:12])),
			Time:     binary.BigEndian.Uint32(rec[12:16]),
		}
	}
	return points
}
