package booxpage

import (
	"encoding/json"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	pb "github.com/jdkruzr/AragoniteAlexandriaServer/internal/booxpage/proto"
)

// summarize describes a shape's structure for coverage diagnostics without
// copying its text, ink or identifiers of other records.
func summarize(sp *pb.ShapeInfoProto) ShapeSummary {
	out := ShapeSummary{CreatedAt: sp.CreatedAt, UpdatedAt: sp.UpdatedAt, TextRunes: utf8.RuneCountInString(sp.Text), RichTextBytes: len(sp.RichText), PointBytes: len(sp.PointList)}
	var b struct{ Left, Top, Right, Bottom float64 }
	if json.Unmarshal([]byte(sp.BoundingRect), &b) == nil {
		out.Width, out.Height = b.Right-b.Left, b.Bottom-b.Top
	}
	for name, v := range map[string]string{"boundingRect": sp.BoundingRect, "matrixValues": sp.MatrixValues, "textStyle": sp.TextStyle, "text": sp.Text, "createArgs": sp.CreateArgs, "groupId": sp.GroupId, "resource": sp.Resource, "lineStyle": sp.LineStyle, "tagIdList": sp.TagIdList, "extra": sp.Extra, "linkIdList": sp.LinkIdList, "richText": sp.RichText, "connectionBean": sp.ConnectionBean, "optionsRepo": sp.OptionsRepo} {
		if v != "" {
			out.Fields = append(out.Fields, name)
		}
	}
	if len(sp.PointList) > 0 {
		out.Fields = append(out.Fields, "pointList")
	}
	sort.Strings(out.Fields)
	out.ExtraKeys = jsonKeys(sp.Extra)
	out.ResourceKeys = jsonKeys(sp.Resource)
	out.OptionKeys = jsonKeys(sp.OptionsRepo)
	out.ConnectionKeys = jsonKeys(sp.ConnectionBean)
	out.CreateArgKeys = jsonKeys(sp.CreateArgs)
	if sp.ShapeType == 6 || sp.ShapeType == 16 {
		var style map[string]any
		if json.Unmarshal([]byte(sp.TextStyle), &style) == nil {
			out.TextStyle = map[string]any{}
			for k, v := range style {
				switch v.(type) {
				case float64, bool, nil:
					out.TextStyle[k] = v
				}
			}
		}
	}
	var ref struct{ RelativePath string }
	if json.Unmarshal([]byte(sp.Resource), &ref) == nil {
		out.ResourceExtension = strings.ToLower(path.Ext(ref.RelativePath))
	}
	return out
}

// jsonKeys lists an object's keys, and one level into nested objects as
// "outer.inner", so new formats are recognizable without their values.
func jsonKeys(raw string) []string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	var out []string
	for k, v := range m {
		out = append(out, k)
		var inner map[string]json.RawMessage
		if json.Unmarshal(v, &inner) == nil {
			for ik := range inner {
				out = append(out, k+"."+ik)
			}
		}
	}
	sort.Strings(out)
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}
