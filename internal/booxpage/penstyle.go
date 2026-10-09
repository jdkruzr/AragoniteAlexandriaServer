// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

// penStyle controls per-pen-type visual treatment.
type penStyle struct {
	PressureExponent float64 // Power curve for pressure response
	MinWidthFactor   float64 // Minimum width as fraction of thickness
	MaxWidthFactor   float64 // Maximum width as fraction of thickness
	AlphaMultiplier  float64 // Opacity multiplier (1.0 = full, <1.0 = translucent)
}

var penStyles = map[int32]penStyle{
	// Pencil (type 2): thin, uniform, low pressure response
	2: {PressureExponent: 0.8, MinWidthFactor: 0.3, MaxWidthFactor: 0.7, AlphaMultiplier: 1.0},
	// Oily/gel pen (type 3): moderate pressure response
	3: {PressureExponent: 0.6, MinWidthFactor: 0.4, MaxWidthFactor: 1.0, AlphaMultiplier: 1.0},
	// Fountain pen (type 4): strong pressure response, wide range
	4: {PressureExponent: 0.4, MinWidthFactor: 0.2, MaxWidthFactor: 1.2, AlphaMultiplier: 1.0},
	// Brush (type 5): very wide range, strong response
	5: {PressureExponent: 0.3, MinWidthFactor: 0.1, MaxWidthFactor: 1.5, AlphaMultiplier: 1.0},
	// Marker (type 15): wide, semi-transparent
	15: {PressureExponent: 0.9, MinWidthFactor: 0.8, MaxWidthFactor: 1.2, AlphaMultiplier: 0.4},
	// Neo brush (type 21): wide brush
	21: {PressureExponent: 0.3, MinWidthFactor: 0.15, MaxWidthFactor: 1.6, AlphaMultiplier: 1.0},
	// Charcoal (type 22): textured, moderate
	22: {PressureExponent: 0.5, MinWidthFactor: 0.3, MaxWidthFactor: 1.0, AlphaMultiplier: 0.85},
	// Square pen (type 47): uniform width
	47: {PressureExponent: 1.0, MinWidthFactor: 0.9, MaxWidthFactor: 1.1, AlphaMultiplier: 1.0},
	// Latin calligraphy (type 60): angle-sensitive approximated as strong pressure
	60: {PressureExponent: 0.35, MinWidthFactor: 0.15, MaxWidthFactor: 1.4, AlphaMultiplier: 1.0},
	// Asian calligraphy (type 61): similar to Latin
	61: {PressureExponent: 0.35, MinWidthFactor: 0.15, MaxWidthFactor: 1.4, AlphaMultiplier: 1.0},
}

// Default pen style for unknown shape types.
var defaultPenStyle = penStyle{
	PressureExponent: 0.5,
	MinWidthFactor:   0.3,
	MaxWidthFactor:   1.0,
	AlphaMultiplier:  1.0,
}

func getPenStyle(shapeType int32) penStyle {
	if ps, ok := penStyles[shapeType]; ok {
		return ps
	}
	return defaultPenStyle
}
