package crema

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Paint renders a layout tree to an RGBA image.
func Paint(root *Box) *image.RGBA {
	// Find actual content height to avoid encoding large blank areas
	contentH := findContentBottom(root)
	if contentH < root.H {
		root.H = contentH + 16 // small padding
	}
	if root.H < 100 {
		root.H = 100
	}

	img := image.NewRGBA(image.Rect(0, 0, root.W, root.H))

	// Fill background
	fillRect(img, 0, 0, root.W, root.H, root.Style.BGColor)

	// Paint all boxes
	paintBox(img, root)

	return img
}

// findContentBottom returns the Y position of the bottom of the last content box.
func findContentBottom(box *Box) int {
	bottom := box.Y + box.H
	for _, child := range box.Children {
		cb := findContentBottom(child)
		if cb > bottom {
			bottom = cb
		}
	}
	return bottom
}

// Screenshot takes a screenshot of the page and returns PNG bytes.
func (p *Page) Screenshot() ([]byte, error) {
	return p.ScreenshotSize(defaultWidth, defaultHeight)
}

// ScreenshotSize takes a screenshot at a specific viewport size.
func (p *Page) ScreenshotSize(width, height int) ([]byte, error) {
	if p.Doc == nil {
		return nil, nil
	}

	root := Layout(p.Doc, width, height)
	p.LastLayout = root
	img := Paint(root)

	var buf bytes.Buffer
	buf.Grow(img.Stride * img.Rect.Max.Y / 4) // estimate compressed size
	err := fastPNG(&buf, img)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ScreenshotFile saves a screenshot to a file.
func (p *Page) ScreenshotFile(path string) error {
	return p.ScreenshotFileSize(path, defaultWidth, defaultHeight)
}

// ScreenshotFileSize saves a screenshot to a file at a specific viewport size.
func (p *Page) ScreenshotFileSize(path string, width, height int) error {
	if p.Doc == nil {
		return nil
	}

	root := Layout(p.Doc, width, height)
	img := Paint(root)

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return fastPNG(f, img)
}

// ScreenshotWriter writes a screenshot to any io.Writer.
func (p *Page) ScreenshotWriter(w io.Writer) error {
	if p.Doc == nil {
		return nil
	}
	root := Layout(p.Doc, defaultWidth, defaultHeight)
	img := Paint(root)
	return fastPNG(w, img)
}

// fastPNG encodes with speed-optimized compression.
var pngEncoder = &png.Encoder{CompressionLevel: png.BestSpeed}

func fastPNG(w io.Writer, img *image.RGBA) error {
	return pngEncoder.Encode(w, img)
}

// ─── Painting ───────────────────────────────────────────────

func paintBox(img *image.RGBA, box *Box) {
	// Background
	if box.Style.BGColor.A > 0 {
		fillRect(img, box.X, box.Y, box.W, box.H, box.Style.BGColor)
	}

	// Border
	if box.Style.BorderW > 0 {
		drawBorder(img, box.X, box.Y, box.W, box.H, box.Style.BorderW, box.Style.BorderColor)
	}

	// Text
	if box.Text != "" {
		textX := box.X + box.Style.PaddingL
		textY := box.Y + box.Style.PaddingT

		textColor := box.Style.Color
		if box.Link != "" {
			textColor = colorBlue
		}

		fontSize := box.Style.FontSize
		if fontSize < 10 {
			fontSize = defaultFontSize
		}
		maxW := box.W - box.Style.PaddingL - box.Style.PaddingR
		if maxW < 40 { maxW = box.W }

		// Center text vertically for buttons
		if box.Element != nil && (box.Element.TagName == "BUTTON" || box.InputType == "submit") {
			tw := measureText(box.Text, fontSize, box.Style.Bold)
			textX = box.X + (box.W-tw)/2
			lh := fontLineHeight(fontSize)
			textY = box.Y + (box.H-lh)/2
		}

		drawTextStyled(img, textX, textY, box.Text, textColor, fontSize, box.Style.Bold, maxW)

		// Underline for links
		if box.Style.Underline || box.Link != "" {
			tw := measureText(box.Text, fontSize, box.Style.Bold)
			if tw > maxW { tw = maxW }
			lh := fontLineHeight(fontSize)
			drawHLine(img, textX, textY+lh-2, tw, textColor)
		}
	}

	// Input field rendering
	if box.InputType != "" {
		switch box.InputType {
		case "checkbox":
			cx := box.X + 2
			cy := box.Y + 2
			drawBorder(img, cx, cy, 16, 16, 1, colorInputBorder)
			fillRect(img, cx+1, cy+1, 14, 14, colorInputBG)
		case "radio":
			cx := box.X + 2
			cy := box.Y + 2
			drawBorder(img, cx, cy, 16, 16, 1, colorInputBorder)
			fillRect(img, cx+1, cy+1, 14, 14, colorInputBG)
		default:
			if box.Placeholder != "" {
				drawTextStyled(img, box.X+10, box.Y+8, box.Placeholder, colorGray, defaultFontSize-2, false, box.W-20)
			}
		}
	}

	// Paint children
	for _, child := range box.Children {
		paintBox(img, child)
	}
}

// ─── Drawing primitives ─────────────────────────────────────

func drawTextStyled(img *image.RGBA, x, y int, text string, c Color, fontSize int, bold bool, maxW int) {
	face := getFace(fontSize, bold)
	if face == nil {
		return
	}

	col := color.RGBA{c.R, c.G, c.B, c.A}
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(col),
		Face: face,
	}

	// Word wrap using actual font metrics
	lines := wrapTextByWidth(text, face, maxW)
	ascent := fontAscent(fontSize, bold)
	lh := fontLineHeight(fontSize)

	for i, line := range lines {
		ly := y + ascent + i*lh
		if ly > img.Bounds().Max.Y {
			break
		}
		d.Dot = fixed.P(x, ly)
		d.DrawString(line)
	}
}

// wrapTextByWidth wraps text using actual font measurement.
func wrapTextByWidth(text string, face font.Face, maxW int) []string {
	if maxW <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	line := words[0]

	for _, w := range words[1:] {
		candidate := line + " " + w
		cw := font.MeasureString(face, candidate).Round()
		if cw > maxW && line != "" {
			lines = append(lines, line)
			line = w
		} else {
			line = candidate
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func fillRect(img *image.RGBA, x, y, w, h int, c Color) {
	bounds := img.Bounds()
	maxX := bounds.Max.X
	maxY := bounds.Max.Y
	stride := img.Stride

	// Clip
	x0 := x
	y0 := y
	x1 := x + w
	y1 := y + h
	if x0 < 0 { x0 = 0 }
	if y0 < 0 { y0 = 0 }
	if x1 > maxX { x1 = maxX }
	if y1 > maxY { y1 = maxY }
	if x0 >= x1 || y0 >= y1 {
		return
	}

	// Build one row of pixels
	rowW := x1 - x0
	row := make([]byte, rowW*4)
	for i := 0; i < rowW; i++ {
		off := i * 4
		row[off] = c.R
		row[off+1] = c.G
		row[off+2] = c.B
		row[off+3] = c.A
	}

	// Copy row to each scanline
	for py := y0; py < y1; py++ {
		off := py*stride + x0*4
		copy(img.Pix[off:off+rowW*4], row)
	}
}

func drawBorder(img *image.RGBA, x, y, w, h, bw int, c Color) {
	// Top
	fillRect(img, x, y, w, bw, c)
	// Bottom
	fillRect(img, x, y+h-bw, w, bw, c)
	// Left
	fillRect(img, x, y, bw, h, c)
	// Right
	fillRect(img, x+w-bw, y, bw, h, c)
}

func drawHLine(img *image.RGBA, x, y, w int, c Color) {
	fillRect(img, x, y, w, 1, c)
}

func setPixel(img *image.RGBA, x, y int, c color.RGBA) {
	bounds := img.Bounds()
	if x >= 0 && y >= 0 && x < bounds.Max.X && y < bounds.Max.Y {
		off := y*img.Stride + x*4
		img.Pix[off] = c.R
		img.Pix[off+1] = c.G
		img.Pix[off+2] = c.B
		img.Pix[off+3] = c.A
	}
}
