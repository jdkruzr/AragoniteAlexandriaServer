// Selectively ported from UltraBridge under Apache-2.0.
package booxpage

// decodeARGB extracts RGBA components from a packed ARGB int32.
// Boox stores color as 0xAARRGGBB.
// Note: When argb is negative (high bit set), right-shift performs arithmetic right-shift,
// replicating the sign bit. The masking operation (&0xFF) isolates the low 8 bits,
// effectively treating the result as an unsigned byte regardless of sign extension.
func decodeARGB(argb int32) (r, g, b, a float64) {
	a = float64((argb>>24)&0xFF) / 255.0
	r = float64((argb>>16)&0xFF) / 255.0
	g = float64((argb>>8)&0xFF) / 255.0
	b = float64(argb&0xFF) / 255.0
	return
}
