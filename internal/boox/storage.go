package boox

import "context"

// This is the native compatibility quota, not a measurement of filesystem free
// space. Usage counts current logical resource keys, excluding retained history.
const nativeStorageLimit int64 = 1 << 40

func (s Service) storageStatus(ctx context.Context) (map[string]any, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT split_part(l.native_key,'/',2),sum(v.bytes) FROM boox_blob_live l JOIN boox_blob_version v ON v.id=l.version_id WHERE NOT v.deleted AND starts_with(l.native_key,$1) GROUP BY 1`, s.uid()+"/")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	categories := map[string]any{}
	var used int64
	for rows.Next() {
		var domain string
		var size int64
		if e = rows.Scan(&domain, &size); e != nil {
			return nil, e
		}
		used += size
		name := map[string]string{"note": "Notebook resources", "reader": "Reading resources", "calendar": "Calendar resources"}[domain]
		if name == "" {
			name = domain + " resources"
		}
		categories[domain] = map[string]any{"name": name, "data": size}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	total := nativeStorageLimit
	if used > total {
		total = used
	}
	categories["left"] = map[string]any{"name": "Available", "data": total - used}
	// Launcher CloudStorageStatusBean consumes this object directly; adding the
	// generic account envelope creates another data level and breaks deserialization.
	return map[string]any{"total": total, "data": categories}, nil
}
