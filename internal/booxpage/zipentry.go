package booxpage

import (
	"archive/zip"
	"compress/flate"
	"errors"
	"hash/crc32"
	"io"
)

// readEntry reads one archive entry of at most limit bytes. Some older
// firmware wrote archives whose data descriptor sits a few bytes past the
// compressed size the central directory records, so archive/zip reads a
// garbage checksum from the descriptor. The central directory itself is
// right; such an entry is accepted when the inflated data matches the
// central directory's size and CRC.
func readEntry(f *zip.File, limit int64) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	r.Close()
	if int64(len(b)) > limit {
		return nil, errors.New("entry too large")
	}
	if err == nil {
		return b, nil
	}
	if !errors.Is(err, zip.ErrChecksum) || f.Flags&0x8 == 0 || f.Method != zip.Deflate {
		return nil, err
	}
	raw, e := f.OpenRaw()
	if e != nil {
		return nil, err
	}
	fr := flate.NewReader(raw)
	b, e = io.ReadAll(io.LimitReader(fr, limit+1))
	fr.Close()
	if e != nil || int64(len(b)) > limit || uint64(len(b)) != f.UncompressedSize64 || crc32.ChecksumIEEE(b) != f.CRC32 {
		return nil, err
	}
	return b, nil
}
