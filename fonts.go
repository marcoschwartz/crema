package crema

import (
	_ "embed"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
)

//go:embed fonts/DejaVuSans.ttf
var fontRegularData []byte

//go:embed fonts/DejaVuSans-Bold.ttf
var fontBoldData []byte

var (
	fontRegular *opentype.Font
	fontBold    *opentype.Font
	fontOnce    sync.Once
	faceCache   = map[faceKey]font.Face{}
	faceMu      sync.Mutex
)

type faceKey struct {
	size int
	bold bool
}

func initFonts() {
	fontOnce.Do(func() {
		var err error
		fontRegular, err = opentype.Parse(fontRegularData)
		if err != nil {
			panic("crema: failed to parse regular font: " + err.Error())
		}
		fontBold, err = opentype.Parse(fontBoldData)
		if err != nil {
			panic("crema: failed to parse bold font: " + err.Error())
		}
	})
}

// getFace returns a cached font.Face for the given size and weight.
func getFace(size int, bold bool) font.Face {
	if size < 8 {
		size = 8
	}
	key := faceKey{size, bold}
	faceMu.Lock()
	defer faceMu.Unlock()

	if f, ok := faceCache[key]; ok {
		return f
	}

	initFonts()

	ft := fontRegular
	if bold {
		ft = fontBold
	}

	face, err := opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    float64(size),
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		// Fallback: try without hinting
		face, _ = opentype.NewFace(ft, &opentype.FaceOptions{
			Size: float64(size),
			DPI:  72,
		})
	}

	faceCache[key] = face
	return face
}

// measureText returns the width of text at a given font size.
func measureText(text string, size int, bold bool) int {
	face := getFace(size, bold)
	if face == nil {
		return len(text) * (size * 6 / 10) // rough fallback
	}
	w := font.MeasureString(face, text)
	return w.Round()
}

// fontAscent returns the ascent for a given font size.
func fontAscent(size int, bold bool) int {
	face := getFace(size, bold)
	if face == nil {
		return size
	}
	metrics := face.Metrics()
	return metrics.Ascent.Round()
}

// fontLineHeight returns the line height for a given font size.
func fontLineHeight(size int) int {
	return size * 14 / 10 // 1.4x line height
}
