// Package contract is the Alexandria reader domain contract: the 15-table reader
// registry, the combined writer+reader schema hash, row validation, domain checks,
// ink fingerprints and the annotation projection. It is parity-tested against the
// Kotlin client through the Stage 2 vector files.
// Copied from UltraBridge (internal/readercontract) under Apache-2.0; keep it in
// step with that source rather than reinterpreting rules here.
// Rhizome owns generic transport/registry mechanics; this package owns the rules.
package contract

import "github.com/jdkruzr/rhizome/server-go/registry"

func Registry() registry.Registry {
	t := func(n string) registry.Column { return registry.Column{Name: n, Type: registry.Text} }
	i := func(n string) registry.Column { return registry.Column{Name: n, Type: registry.Int} }
	n := func(n string) registry.Column { return registry.Column{Name: n, Type: registry.Text, Nullable: true} }
	b := func(n string, nullable bool) registry.Column {
		return registry.Column{Name: n, Type: registry.Blob, Nullable: nullable}
	}
	table := func(n string, c ...registry.Column) registry.Table {
		return registry.Table{Name: n, PK: "id", Columns: c}
	}
	return registry.Registry{Tables: []registry.Table{
		table("reader_book", t("asset_id"), i("byte_length"), t("media_type"), t("metadata_json")),
		table("reader_book_title", t("title")),
		table("reader_book_lifecycle", i("deleted"), i("changed_at")),
		table("reader_annotation", t("book_id"), t("initial_anchor_json"), i("canvas_width"), i("initial_height"), t("creator_session_id")),
		table("reader_edit_session", t("annotation_id"), t("kind"), n("owner_site"), t("state")),
		table("reader_stroke", t("annotation_id"), t("session_id"), i("paint_order"), t("paint_site"),
			registry.Column{Name: "color", Type: registry.ColorInt}, i("pen_width_min"), i("pen_width_max"), t("brush_kind"),
			i("brush_version"), i("brush_seed"), b("points", false), b("point_dynamics", true)),
		table("reader_erase_claim", t("session_id"), t("stroke_id"), i("active")),
		table("reader_annotation_value", t("session_id"), t("property"), t("value_json")),
		table("reader_annotation_lifecycle", i("deleted"), i("changed_at")),
		table("reader_position", t("book_id"), t("site_id"), t("locator_json")),
		table("reader_recognition", t("annotation_id"), t("producer_id"), t("input_hash"), t("engine"), n("model"), n("language"), t("status"), t("text")),
		table("content_anchor", t("selector_json"), n("label")),
		table("content_reference", t("source_anchor_id"), t("target_anchor_id"), n("label")),
		table("content_anchor_lifecycle", i("deleted"), i("changed_at")),
		table("content_reference_lifecycle", i("deleted"), i("changed_at")),
	}}
}

// CandidateCombined leaves the production ForestNote registry and accepted hash set untouched.
func CandidateCombined() registry.Registry {
	r := registry.ForestNote()
	r.Tables = append(r.Tables, Registry().Tables...)
	return r
}
