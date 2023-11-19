package markdown

type Options func(r *renderer)

// DitheringMode type is used for image scale dithering mode constants.
type DitheringMode uint8

const (
	NoDithering = DitheringMode(iota)
	DitheringWithBlocks
	DitheringWithChars
)

// Use a custom collection of ANSI colors for the headings
func WithHeadingShades(shades []shadeFmt) Options {
	return func(r *renderer) {
		r.headingShade = shade(shades)
	}
}

// Use a custom collection of ANSI colors for the blockquotes
func WithBlockquoteShades(shades []shadeFmt) Options {
	return func(r *renderer) {
		r.blockQuoteShade = shade(shades)
	}
}
