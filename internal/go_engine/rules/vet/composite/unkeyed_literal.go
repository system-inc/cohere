package composite

// unkeyedLiteral is the allow list of standard-library types whose unkeyed literals are deemed acceptable, keyed by
// the type as it prints. The whitelist option set false turns it off. x/tools' composite unkeyedLiteral, from
// whitelist.go, entry for entry.
var unkeyedLiteral = map[string]bool{
	// These image and image/color struct types are frozen. We will never add fields to them.
	"image/color.Alpha16": true,
	"image/color.Alpha":   true,
	"image/color.CMYK":    true,
	"image/color.Gray16":  true,
	"image/color.Gray":    true,
	"image/color.NRGBA64": true,
	"image/color.NRGBA":   true,
	"image/color.NYCbCrA": true,
	"image/color.RGBA64":  true,
	"image/color.RGBA":    true,
	"image/color.YCbCr":   true,
	"image.Point":         true,
	"image.Rectangle":     true,
	"image.Uniform":       true,

	"unicode.Range16": true,
	"unicode.Range32": true,

	// These four structs are used in generated test main files,
	// but the generator can be trusted.
	"testing.InternalBenchmark":  true,
	"testing.InternalExample":    true,
	"testing.InternalTest":       true,
	"testing.InternalFuzzTarget": true,
}
