package booxpage

import "testing"

func stroke(id string, color int32, x float32) *Shape {
	return &Shape{UniqueID: id, ShapeType: 2, Color: color, Thickness: 3, Points: []TinyPoint{{X: x, Y: 50, Pressure: 2000}, {X: x + 40, Y: 60, Pressure: 2000}, {X: x + 80, Y: 50, Pressure: 2000}}}
}

func TestInkFilter(t *testing.T) {
	black := stroke("k", -16777216, 10)
	red := stroke("r", InkRed, 200)
	recovered := stroke("x", InkRed, 300)
	recovered.Recovered = true
	marker := stroke("m", InkRed, 400)
	marker.ShapeType = shapeMarker
	p := &Page{Width: 600, Height: 200, Shapes: []*Shape{black, red, recovered, marker}}
	got := InkShapes(p, InkRed)
	if len(got) != 1 || got[0] != red {
		t.Fatalf("red ink = %v", got)
	}
	if len(InkShapes(p, InkBlue)) != 0 {
		t.Fatal("no blue ink expected")
	}
	img := InkImage(p, got)
	if img == nil {
		t.Fatal("no image")
	}
	// Cropped to the red stroke (x 200–280) plus margins, not the page.
	if w := img.Bounds().Dx(); w > 200 {
		t.Fatalf("not cropped: width %d", w)
	}
	h1 := InkHash(got)
	other := *red
	other.Points = append([]TinyPoint{}, red.Points...)
	other.Points[1].Y = 70
	if h1 == InkHash([]*Shape{&other}) || h1 != InkHash(InkShapes(p, InkRed)) {
		t.Fatal("ink hash should follow the strokes only")
	}
	if InkImage(p, nil) != nil {
		t.Fatal("no ink, no image")
	}
}
