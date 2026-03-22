package crema

import (
	"strconv"
	"strings"
)

// ─── Layout Engine ──────────────────────────────────────────
// Minimal CSS box model: block elements stack vertically,
// inline elements flow horizontally with wrapping.

// Box represents a laid-out rectangle on screen.
type Box struct {
	X, Y, W, H int
	Element     *Element
	Text        string      // for text boxes
	Children    []*Box
	Style       BoxStyle
	IsText      bool
	IsInline    bool
	Link        string      // href for <a> tags
	InputType   string      // for <input> elements
	Placeholder string      // for <input> placeholder
	Bullet      string      // bullet marker for list items
}

// BoxStyle holds computed visual properties.
type BoxStyle struct {
	BGColor    Color
	Color      Color
	FontSize   int
	Bold       bool
	Italic     bool
	Underline  bool
	PaddingT   int
	PaddingR   int
	PaddingB   int
	PaddingL   int
	MarginT    int
	MarginR    int
	MarginB    int
	MarginL    int
	BorderW    int
	BorderColor Color
	Display    string // "block", "inline", "none", "flex"
	Hidden     bool
	// Flexbox
	FlexDirection  string // "row", "column"
	JustifyContent string // "flex-start", "center", "flex-end", "space-between", "space-around", "space-evenly"
	AlignItems     string // "flex-start", "center", "flex-end", "stretch"
	FlexWrap       string // "nowrap", "wrap"
	Gap            int
	FlexGrow       float64
	FlexShrink     float64
}

// Color is an RGBA color.
type Color struct {
	R, G, B, A uint8
}

var (
	colorWhite       = Color{255, 255, 255, 255}
	colorBlack       = Color{0, 0, 0, 255}
	colorDarkGray    = Color{51, 51, 51, 255}
	colorGray        = Color{128, 128, 128, 255}
	colorLightGray   = Color{240, 240, 240, 255}
	colorMediumGray  = Color{220, 220, 220, 255}
	colorBlue        = Color{0, 102, 204, 255}
	colorBtnBG       = Color{66, 133, 244, 255}   // Google-style blue button
	colorBtnText     = Color{255, 255, 255, 255}
	colorBtnBorder   = Color{55, 120, 220, 255}
	colorInputBG     = Color{255, 255, 255, 255}
	colorInputBorder = Color{185, 185, 185, 255}
	colorNavBG       = Color{50, 50, 50, 255}
	colorNavText     = Color{230, 230, 230, 255}
	colorFooterBG    = Color{245, 245, 245, 255}
)

const (
	defaultFontSize  = 16
	defaultWidth     = 1280
	defaultHeight    = 800
	maxContentWidth  = 800
	charWidth        = 8  // fallback
	bodyPaddingX     = 24
	bodyPaddingY     = 16
)

// Layout computes the position and size of all visible elements.
func Layout(doc *Document, viewportW, viewportH int) *Box {
	root := &Box{
		X: 0, Y: 0,
		W: viewportW, H: viewportH,
		Style: BoxStyle{BGColor: colorWhite, Color: colorDarkGray, FontSize: defaultFontSize},
	}

	body := findChildElement(&doc.Node, "BODY")
	if body == nil {
		return root
	}

	// Center content with max width
	contentW := viewportW
	if contentW > maxContentWidth+bodyPaddingX*2 {
		contentW = maxContentWidth
	} else {
		contentW -= bodyPaddingX * 2
	}
	contentX := (viewportW - contentW) / 2

	y := bodyPaddingY
	layoutChildren(body, root, contentX, &y, contentW, viewportW)

	// Adjust root height to content
	if y+bodyPaddingY > viewportH {
		root.H = y + bodyPaddingY
	}

	return root
}

func layoutChildren(el *Element, parent *Box, x int, y *int, availW int, viewportW int) {
	// Collect inline elements into runs, then lay them out together
	i := 0
	for i < len(el.Children) {
		child := el.Children[i]

		// Text node
		if tn := nodeToText(child); tn != nil {
			text := strings.TrimSpace(tn.Data)
			if text != "" {
				textBox := layoutText(text, parent, x, *y, availW)
				parent.Children = append(parent.Children, textBox)
				*y += textBox.H
			}
			i++
			continue
		}

		cel := nodeToElement(child)
		if cel == nil {
			i++
			continue
		}

		style := computeStyle(cel, parent)
		if style.Hidden || style.Display == "none" {
			i++
			continue
		}

		// Collect consecutive inline elements into a single line
		if style.Display == "inline" {
			inlineX := x
			maxH := 0
			for i < len(el.Children) {
				child = el.Children[i]
				// Check for text node between inlines
				if tn := nodeToText(child); tn != nil {
					text := strings.TrimSpace(tn.Data)
					if text != "" {
						fontSize := parent.Style.FontSize
						if fontSize < 10 { fontSize = defaultFontSize }
						tw := measureText(text, fontSize, parent.Style.Bold)
						lh := fontLineHeight(fontSize)
						// Wrap to next line if needed
						if inlineX+tw > x+availW && inlineX > x {
							*y += maxH
							inlineX = x
							maxH = 0
						}
						tb := &Box{
							X: inlineX, Y: *y, W: tw, H: lh,
							Text: text, IsText: true, IsInline: true,
							Style: parent.Style,
						}
						parent.Children = append(parent.Children, tb)
						inlineX += tw
						if lh > maxH { maxH = lh }
					}
					i++
					continue
				}

				cel = nodeToElement(child)
				if cel == nil {
					i++
					continue
				}

				istyle := computeStyle(cel, parent)
				if istyle.Display != "inline" {
					break // back to block flow
				}
				if istyle.Hidden {
					i++
					continue
				}

				text := extractPlainText(cel)
				if text != "" {
					tw := measureText(text, istyle.FontSize, istyle.Bold)
					lh := fontLineHeight(istyle.FontSize)
					// Wrap to next line if needed
					if inlineX+tw > x+availW && inlineX > x {
						*y += maxH
						inlineX = x
						maxH = 0
					}
					box := &Box{
						X: inlineX, Y: *y,
						W: tw + istyle.PaddingL + istyle.PaddingR,
						H: lh + istyle.PaddingT + istyle.PaddingB,
						Element: cel, Text: text, IsInline: true,
						Style: istyle,
					}
					if cel.TagName == "A" {
						box.Link = cel.GetAttribute("href")
					}
					if cel.TagName == "BUTTON" {
						box.W = tw + 40
						box.H = lh + 18
						box.Style.BGColor = colorBtnBG
						box.Style.Color = colorBtnText
						box.Style.Bold = true
					}
					parent.Children = append(parent.Children, box)
					inlineX += box.W + 4 // small gap between inline elements
					if box.H > maxH { maxH = box.H }
				}
				i++
			}
			if maxH > 0 {
				*y += maxH
			}
			continue
		}

		// Block element
		i++
		layoutBlock(cel, parent, x, y, availW, viewportW, style)
	}
}

func layoutBlock(cel *Element, parent *Box, x int, y *int, availW int, viewportW int, style BoxStyle) {
	*y += style.MarginT
	box := &Box{
		Element: cel,
		Style:   style,
	}

	// Full-width elements (nav, header, footer) span the viewport
	if cel.TagName == "NAV" || cel.TagName == "HEADER" {
		box.X = 0
		box.Y = *y
		box.W = viewportW
	} else if cel.TagName == "FOOTER" {
		box.X = 0
		box.Y = *y
		box.W = viewportW
	} else {
		box.X = x + style.MarginL
		box.Y = *y
		box.W = availW - style.MarginL - style.MarginR
	}

	innerY := *y + style.PaddingT
	innerX := box.X + style.PaddingL
	innerW := box.W - style.PaddingL - style.PaddingR
	if innerW < 20 { innerW = 20 }

	// Flex layout
	if style.Display == "flex" {
		layoutFlex(cel, box, innerX, &innerY, innerW, viewportW, style)
		box.H = (innerY - *y) + style.PaddingB
		lh := fontLineHeight(style.FontSize)
		if box.H < lh { box.H = lh }
		parent.Children = append(parent.Children, box)
		*y = box.Y + box.H + style.MarginB
		return
	}

	switch cel.TagName {
	case "INPUT":
		box.InputType = cel.GetAttribute("type")
		if box.InputType == "" { box.InputType = "text" }
		box.Placeholder = cel.GetAttribute("placeholder")
		if box.Placeholder == "" {
			box.Placeholder = cel.GetAttribute("value")
		}
		lh := fontLineHeight(style.FontSize)
		switch box.InputType {
		case "checkbox", "radio":
			box.W = 20
			box.H = 20
		case "submit":
			// Render submit inputs like buttons
			text := cel.GetAttribute("value")
			if text == "" { text = "Submit" }
			box.Text = text
			tw := measureText(text, style.FontSize, true)
			box.W = tw + 40
			box.H = lh + 18
			box.Style.BGColor = colorBtnBG
			box.Style.Color = colorBtnText
			box.Style.Bold = true
			box.Style.BorderW = 0
		default:
			box.W = innerW
			box.H = lh + 18
			box.Style.BorderW = 1
			box.Style.BorderColor = colorInputBorder
			box.Style.BGColor = colorInputBG
			box.Style.PaddingL = 10
			box.Style.PaddingT = 4
		}
		parent.Children = append(parent.Children, box)
		*y += box.H + style.MarginB + 6
		return

	case "BUTTON":
		text := extractPlainText(cel)
		if text == "" { text = "Button" }
		box.Text = text
		lh := fontLineHeight(style.FontSize)
		tw := measureText(text, style.FontSize, true)
		box.W = tw + 40
		box.H = lh + 18
		box.Style.BGColor = colorBtnBG
		box.Style.Color = colorBtnText
		box.Style.Bold = true
		box.Style.BorderW = 0
		parent.Children = append(parent.Children, box)
		*y += box.H + style.MarginB + 6
		return

	case "IMG":
		alt := cel.GetAttribute("alt")
		if alt == "" { alt = "[image]" }
		box.Text = alt
		box.H = 80
		w := 240
		if ws := cel.GetAttribute("width"); ws != "" {
			if n, err := strconv.Atoi(ws); err == nil { w = n }
		}
		box.W = w
		box.Style.BGColor = colorLightGray
		box.Style.BorderW = 1
		box.Style.BorderColor = colorMediumGray
		box.Style.PaddingT = 8
		box.Style.PaddingL = 8
		parent.Children = append(parent.Children, box)
		*y += box.H + style.MarginB
		return

	case "HR":
		box.H = 1
		box.Style.BGColor = colorMediumGray
		parent.Children = append(parent.Children, box)
		*y += box.H + 16
		return

	case "BR":
		*y += fontLineHeight(style.FontSize)
		return

	case "LI":
		inNav := isInsideTag(cel, "NAV") || (isInsideTag(cel, "HEADER") && isParentNavLike(cel))
		if !inNav {
			// Add bullet marker (not in nav menus)
			box.Bullet = "\u2022 "
			// Check if parent is OL
			if cel.Parent != nil {
				if pel := nodeToElement(cel.Parent); pel != nil && pel.TagName == "OL" {
					idx := 1
					for _, sib := range cel.Parent.Children {
						if sib == &cel.Node { break }
						if sel := nodeToElement(sib); sel != nil && sel.TagName == "LI" { idx++ }
					}
					box.Bullet = strconv.Itoa(idx) + ". "
				}
			}
		}
	}

	// Layout children recursively
	layoutChildren(cel, box, innerX, &innerY, innerW, viewportW)

	// If no children laid out content, render own text
	if len(box.Children) == 0 {
		text := extractDirectText(cel)
		if text != "" {
			if box.Bullet != "" {
				text = box.Bullet + text
			}
			tb := layoutText(text, box, innerX, innerY, innerW)
			box.Children = append(box.Children, tb)
			innerY += tb.H
		}
	} else if box.Bullet != "" && len(box.Children) > 0 {
		// Prepend bullet to first text child
		for _, ch := range box.Children {
			if ch.IsText && ch.Text != "" {
				ch.Text = box.Bullet + ch.Text
				break
			}
		}
	}

	box.H = (innerY - *y) + style.PaddingB
	lh := fontLineHeight(style.FontSize)
	if box.H < lh {
		box.H = lh
	}

	parent.Children = append(parent.Children, box)
	*y = box.Y + box.H + style.MarginB
}

// ─── Flexbox Layout ─────────────────────────────────────────

type flexChild struct {
	node    *Node
	el      *Element
	style   BoxStyle
	box     *Box
	minW    int
	minH    int
	grow    float64
}

func layoutFlex(el *Element, parent *Box, x int, y *int, availW int, viewportW int, style BoxStyle) {
	dir := style.FlexDirection
	if dir == "" { dir = "row" }
	gap := style.Gap
	wrap := style.FlexWrap == "wrap"

	// First pass: measure all children
	var items []flexChild
	for _, child := range el.Children {
		cel := nodeToElement(child)
		if cel == nil {
			// Text node in flex container
			if tn := nodeToText(child); tn != nil {
				text := strings.TrimSpace(tn.Data)
				if text != "" {
					fontSize := parent.Style.FontSize
					if fontSize < 10 { fontSize = defaultFontSize }
					tw := measureText(text, fontSize, parent.Style.Bold)
					lh := fontLineHeight(fontSize)
					tb := &Box{
						X: 0, Y: 0, W: tw, H: lh,
						Text: text, IsText: true,
						Style: parent.Style,
					}
					items = append(items, flexChild{node: child, box: tb, minW: tw, minH: lh})
				}
			}
			continue
		}

		cs := computeStyle(cel, parent)
		if cs.Hidden || cs.Display == "none" {
			continue
		}

		// Measure child by laying it out in a temp box
		tempBox := &Box{
			X: 0, Y: 0, W: availW,
			Element: cel,
			Style:   cs,
		}
		tempInnerW := availW - cs.PaddingL - cs.PaddingR
		if tempInnerW < 20 { tempInnerW = 20 }

		// Check for special elements
		switch cel.TagName {
		case "INPUT":
			itype := cel.GetAttribute("type")
			if itype == "" { itype = "text" }
			lh := fontLineHeight(cs.FontSize)
			if itype == "submit" {
				text := cel.GetAttribute("value")
				if text == "" { text = "Submit" }
				tw := measureText(text, cs.FontSize, true)
				tempBox.W = tw + 40
				tempBox.H = lh + 18
				tempBox.Text = text
				tempBox.InputType = itype
			} else if itype == "checkbox" || itype == "radio" {
				tempBox.W = 20
				tempBox.H = 20
				tempBox.InputType = itype
			} else {
				tempBox.W = tempInnerW
				tempBox.H = lh + 18
				tempBox.InputType = itype
				tempBox.Placeholder = cel.GetAttribute("placeholder")
				tempBox.Style.BorderW = 1
				tempBox.Style.BorderColor = colorInputBorder
				tempBox.Style.BGColor = colorInputBG
				tempBox.Style.PaddingL = 10
			}
		case "BUTTON":
			text := extractPlainText(cel)
			if text == "" { text = "Button" }
			lh := fontLineHeight(cs.FontSize)
			tw := measureText(text, cs.FontSize, true)
			tempBox.W = tw + 40
			tempBox.H = lh + 18
			tempBox.Text = text
			tempBox.Style.BGColor = colorBtnBG
			tempBox.Style.Color = colorBtnText
			tempBox.Style.Bold = true
		case "IMG":
			tempBox.W = 240
			tempBox.H = 80
			alt := cel.GetAttribute("alt")
			if alt == "" { alt = "[image]" }
			tempBox.Text = alt
			tempBox.Style.BGColor = colorLightGray
		default:
			// Recursively layout to measure
			tempInnerY := cs.PaddingT
			layoutChildren(cel, tempBox, cs.PaddingL, &tempInnerY, tempInnerW, viewportW)
			if len(tempBox.Children) == 0 {
				text := extractPlainText(cel)
				if text != "" {
					tw := measureText(text, cs.FontSize, cs.Bold)
					lh := fontLineHeight(cs.FontSize)
					tempBox.W = tw + cs.PaddingL + cs.PaddingR
					tempBox.H = lh + cs.PaddingT + cs.PaddingB
					tempBox.Text = text
				}
			} else {
				// Shrink-wrap: compute actual content width from children
				maxRight := 0
				for _, ch := range tempBox.Children {
					right := ch.X + ch.W
					if right > maxRight { maxRight = right }
				}
				contentW := maxRight + cs.PaddingR
				if contentW < tempBox.W { tempBox.W = contentW }
			}
			h := tempInnerY + cs.PaddingB
			lh := fontLineHeight(cs.FontSize)
			if h < lh { h = lh }
			tempBox.H = h
		}

		grow := cs.FlexGrow
		minW := tempBox.W
		// For flex-grow items, use content-based minimum width, not full available width
		if grow > 0 {
			text := extractPlainText(cel)
			if text != "" {
				minW = measureText(text, cs.FontSize, cs.Bold) + cs.PaddingL + cs.PaddingR
			} else if minW > availW/2 {
				minW = 0 // shrink to zero, let grow distribute
			}
		}
		items = append(items, flexChild{node: child, el: cel, style: cs, box: tempBox, minW: minW, minH: tempBox.H, grow: grow})
	}

	if len(items) == 0 {
		return
	}

	if dir == "row" {
		layoutFlexRow(items, parent, x, y, availW, gap, wrap, style)
	} else {
		layoutFlexColumn(items, parent, x, y, availW, gap, style)
	}
}

func layoutFlexRow(items []flexChild, parent *Box, x int, y *int, availW int, gap int, wrap bool, style BoxStyle) {
	// Calculate total min width
	totalMinW := 0
	totalGrow := 0.0
	for i, item := range items {
		totalMinW += item.minW
		totalGrow += item.grow
		if i > 0 { totalMinW += gap }
	}

	// Distribute extra space via flex-grow
	extraW := availW - totalMinW
	if extraW < 0 { extraW = 0 }

	// Calculate item positions
	type rowItem struct {
		item  flexChild
		w, h  int
	}

	var rows [][]rowItem
	var currentRow []rowItem
	rowW := 0

	for i, item := range items {
		w := item.minW
		if totalGrow > 0 && item.grow > 0 {
			w += int(float64(extraW) * item.grow / totalGrow)
		}
		h := item.minH

		if wrap && rowW+w > availW && len(currentRow) > 0 {
			rows = append(rows, currentRow)
			currentRow = nil
			rowW = 0
		}

		currentRow = append(currentRow, rowItem{item: item, w: w, h: h})
		rowW += w
		if i < len(items)-1 { rowW += gap }
	}
	if len(currentRow) > 0 {
		rows = append(rows, currentRow)
	}

	// Layout each row
	for _, row := range rows {
		// Find max height in row
		maxH := 0
		totalRowW := 0
		for i, ri := range row {
			if ri.h > maxH { maxH = ri.h }
			totalRowW += ri.w
			if i > 0 { totalRowW += gap }
		}

		// Calculate starting X based on justify-content
		curX := x
		itemGap := gap
		switch style.JustifyContent {
		case "center":
			curX = x + (availW-totalRowW)/2
		case "flex-end":
			curX = x + availW - totalRowW
		case "space-between":
			if len(row) > 1 {
				itemGap = (availW - totalRowW + gap*(len(row)-1)) / (len(row) - 1)
			}
		case "space-around":
			if len(row) > 0 {
				space := (availW - totalRowW + gap*(len(row)-1)) / (len(row) * 2)
				curX = x + space
				itemGap = space * 2
			}
		case "space-evenly":
			if len(row) > 0 {
				space := (availW - totalRowW + gap*(len(row)-1)) / (len(row) + 1)
				curX = x + space
				itemGap = space
			}
		}

		for i, ri := range row {
			box := ri.item.box
			// Calculate offset from temp layout origin to final position
			offsetX := curX - box.X
			offsetY := *y - box.Y
			box.X = curX
			box.Y = *y

			// Apply align-items
			switch style.AlignItems {
			case "center":
				offsetY = (*y + (maxH-ri.h)/2) - box.Y + offsetY
				box.Y = *y + (maxH-ri.h)/2
			case "flex-end":
				offsetY = (*y + maxH - ri.h) - box.Y + offsetY
				box.Y = *y + maxH - ri.h
			case "stretch":
				box.H = maxH
			}

			box.W = ri.w
			if box.H == 0 { box.H = ri.h }

			// Offset all children to match the final position
			offsetChildren(box, offsetX, offsetY)

			parent.Children = append(parent.Children, box)
			curX += ri.w
			if i < len(row)-1 { curX += itemGap }
		}

		*y += maxH
		if len(rows) > 1 { *y += gap }
	}
}

func layoutFlexColumn(items []flexChild, parent *Box, x int, y *int, availW int, gap int, style BoxStyle) {
	// Column: stack vertically, apply justify/align on vertical/horizontal axes
	totalH := 0
	for i, item := range items {
		totalH += item.minH
		if i > 0 { totalH += gap }
	}

	for i, item := range items {
		box := item.box
		offsetX := x - box.X
		offsetY := *y - box.Y
		box.Y = *y

		// align-items in column = horizontal alignment
		switch style.AlignItems {
		case "center":
			offsetX = (x + (availW-item.minW)/2) - box.X + offsetX
			box.X = x + (availW-item.minW)/2
		case "flex-end":
			offsetX = (x + availW - item.minW) - box.X + offsetX
			box.X = x + availW - item.minW
		case "stretch":
			box.X = x
			box.W = availW
		default:
			box.X = x
		}

		if box.H == 0 { box.H = item.minH }
		offsetChildren(box, offsetX, offsetY)

		parent.Children = append(parent.Children, box)
		*y += box.H
		if i < len(items)-1 { *y += gap }
	}
}

// offsetChildren recursively offsets all child box coordinates.
func offsetChildren(box *Box, dx, dy int) {
	for _, child := range box.Children {
		child.X += dx
		child.Y += dy
		offsetChildren(child, dx, dy)
	}
}

func layoutText(text string, parent *Box, x, y, availW int) *Box {
	fontSize := parent.Style.FontSize
	if fontSize < 10 { fontSize = defaultFontSize }
	bold := parent.Style.Bold

	face := getFace(fontSize, bold)
	var lines []string
	if face != nil {
		lines = wrapTextByWidth(text, face, availW)
	} else {
		charsPerLine := availW / charWidth
		if charsPerLine < 10 { charsPerLine = 10 }
		lines = wrapText(text, charsPerLine)
	}
	lh := fontLineHeight(fontSize)
	h := len(lines) * lh
	if h == 0 { h = lh }

	return &Box{
		X: x, Y: y, W: availW, H: h,
		Text: text, IsText: true,
		Style: parent.Style,
	}
}

func wrapText(text string, maxChars int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > maxChars {
			lines = append(lines, line)
			line = w
		} else {
			line += " " + w
		}
	}
	lines = append(lines, line)
	return lines
}

func extractPlainText(el *Element) string {
	var sb strings.Builder
	CollectTextFromElement(el, &sb)
	return strings.TrimSpace(sb.String())
}

func extractDirectText(el *Element) string {
	var sb strings.Builder
	for _, child := range el.Children {
		if tn := nodeToText(child); tn != nil {
			sb.WriteString(tn.Data)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ─── Style computation ─────────────────────────────────────

func computeStyle(el *Element, parent *Box) BoxStyle {
	s := BoxStyle{
		BGColor:  Color{0, 0, 0, 0}, // transparent
		Color:    parent.Style.Color,
		FontSize: parent.Style.FontSize,
		Display:  "block",
	}

	switch el.TagName {
	case "H1":
		s.FontSize = 32
		s.Bold = true
		s.MarginT = 20
		s.MarginB = 16
		s.Color = colorBlack
	case "H2":
		s.FontSize = 26
		s.Bold = true
		s.MarginT = 24
		s.MarginB = 12
		s.Color = colorBlack
	case "H3":
		s.FontSize = 20
		s.Bold = true
		s.MarginT = 20
		s.MarginB = 10
		s.Color = colorBlack
	case "H4", "H5", "H6":
		s.FontSize = 17
		s.Bold = true
		s.MarginT = 16
		s.MarginB = 8
		s.Color = colorBlack
	case "P":
		s.MarginT = 0
		s.MarginB = 12
	case "DIV":
		s.MarginT = 2
		s.MarginB = 2
	case "SPAN", "SMALL", "SUB", "SUP", "LABEL":
		s.Display = "inline"
	case "A":
		s.Display = "inline"
		if isInsideTag(el, "NAV") || (isInsideTag(el, "HEADER") && isParentNavLike(el)) {
			s.Color = colorNavText
			s.Underline = false
		} else {
			s.Color = colorBlue
			s.Underline = true
		}
	case "STRONG", "B":
		s.Display = "inline"
		s.Bold = true
	case "EM", "I":
		s.Display = "inline"
		s.Italic = true
	case "U":
		s.Display = "inline"
		s.Underline = true
	case "CODE":
		s.Display = "inline"
		s.BGColor = Color{245, 245, 245, 255}
		s.PaddingL = 3
		s.PaddingR = 3
	case "UL", "OL":
		s.PaddingL = 24
		s.MarginT = 8
		s.MarginB = 8
		// UL inside <nav> or <header> where children are navigation links → horizontal
		if isInsideTag(el, "NAV") || (isInsideTag(el, "HEADER") && isNavLikeList(el)) {
			s.Display = "flex"
			s.FlexDirection = "row"
			s.Gap = 6
			s.PaddingL = 0
			s.MarginT = 0
			s.MarginB = 0
			s.AlignItems = "center"
		}
	case "LI":
		s.MarginB = 6
		// LI inside nav-like list → inline, no bullets
		if isInsideTag(el, "NAV") || (isInsideTag(el, "HEADER") && isParentNavLike(el)) {
			s.Display = "inline"
			s.MarginB = 0
		}
	case "NAV", "HEADER":
		s.PaddingT = 12
		s.PaddingB = 12
		s.PaddingL = 24
		s.PaddingR = 24
		s.BGColor = colorNavBG
		s.Color = colorNavText
		s.MarginB = 20
	case "FOOTER":
		s.PaddingT = 16
		s.PaddingB = 16
		s.PaddingL = 24
		s.PaddingR = 24
		s.BGColor = colorFooterBG
		s.MarginT = 24
		s.FontSize = 13
		s.Color = colorGray
	case "SECTION", "ARTICLE", "MAIN":
		s.PaddingT = 4
		s.PaddingB = 4
	case "BLOCKQUOTE":
		s.PaddingL = 16
		s.PaddingT = 8
		s.PaddingB = 8
		s.MarginL = 4
		s.BorderW = 3
		s.BorderColor = colorMediumGray
		s.MarginT = 12
		s.MarginB = 12
		s.Color = colorGray
	case "PRE":
		s.BGColor = Color{245, 245, 245, 255}
		s.PaddingT = 12
		s.PaddingB = 12
		s.PaddingL = 12
		s.PaddingR = 12
		s.MarginT = 8
		s.MarginB = 8
		s.FontSize = 13
	case "TABLE":
		s.BorderW = 1
		s.BorderColor = colorMediumGray
		s.PaddingT = 2
		s.PaddingB = 2
	case "TR":
		s.Display = "flex"
		s.FlexDirection = "row"
		s.Gap = 0
		s.AlignItems = "stretch"
	case "TD", "TH":
		s.PaddingT = 4
		s.PaddingB = 4
		s.PaddingL = 8
		s.PaddingR = 8
		s.FlexGrow = 1
		s.BorderW = 1
		s.BorderColor = colorMediumGray
		if el.TagName == "TH" {
			s.Bold = true
			s.BGColor = colorLightGray
		}
	case "THEAD":
		s.Bold = true
	case "TBODY", "TFOOT":
		// block, no special styling
	case "FORM":
		s.MarginT = 8
		s.MarginB = 8
	case "SCRIPT", "STYLE", "LINK", "META", "HEAD", "NOSCRIPT", "SVG", "PATH", "TEMPLATE":
		s.Hidden = true
		s.Display = "none"
	case "BUTTON":
		s.Display = "inline"
		s.MarginT = 4
		s.MarginB = 4
		s.MarginR = 4
	case "INPUT", "TEXTAREA", "SELECT":
		s.MarginB = 4
	}

	// Parse inline style attribute
	if style := el.GetAttribute("style"); style != "" {
		parseInlineStyle(style, &s)
	}

	if el.HasAttribute("hidden") {
		s.Hidden = true
		s.Display = "none"
	}

	// ── Visibility rules based on web standards and universal conventions ──

	// 1. CSS accessibility classes (Bootstrap, Tailwind, WordPress, Foundation)
	//    These are the standard way to hide content visually while keeping
	//    it accessible to screen readers. Used by virtually all frameworks.
	cls := el.GetAttribute("class")
	if cls != "" {
		for _, hideCls := range []string{
			"sr-only",              // Bootstrap
			"visually-hidden",      // Bootstrap 5+
			"screen-reader-text",   // WordPress
			"screenreader",         // Generic
			"clip-path",            // Modern technique
			"skip",                 // Common prefix for skip links
		} {
			if strings.Contains(cls, hideCls) {
				s.Hidden = true
				s.Display = "none"
				break
			}
		}
	}

	// 2. aria-hidden="true" — W3C WAI-ARIA standard for hiding from all users
	if el.GetAttribute("aria-hidden") == "true" {
		s.Hidden = true
		s.Display = "none"
	}

	// 3. Skip navigation links — <a> with href starting with "#" at top of page.
	//    These are required by WCAG but visually hidden on desktop.
	//    Detected by: anchor linking to a page fragment, typically first elements in body.
	if el.TagName == "A" {
		href := el.GetAttribute("href")
		if strings.HasPrefix(href, "#") && len(href) > 1 {
			// Check if this looks like a skip link (near top of body, links to #content, #main, etc.)
			if isFirstFewChildren(el) {
				s.Hidden = true
				s.Display = "none"
			}
		}
	}

	// 4. Mobile nav toggle buttons — <button> with aria-expanded is a toggle
	//    control (hamburger menu, collapsible panel). On desktop viewport
	//    width (>1024px) these are universally hidden via CSS media queries.
	//    Since we render at desktop width, hide them.
	if el.TagName == "BUTTON" && el.HasAttribute("aria-expanded") {
		s.Hidden = true
		s.Display = "none"
	}

	// 5. Mobile menu containers — divs with "mobile-menu" in id/class
	//    are hidden on desktop via CSS media queries.
	id := el.GetAttribute("id")
	if id != "" && strings.Contains(strings.ToLower(id), "mobile-menu") {
		s.Hidden = true
		s.Display = "none"
	}
	if cls != "" && strings.Contains(strings.ToLower(cls), "mobile-menu") {
		s.Hidden = true
		s.Display = "none"
	}

	return s
}

// isFirstFewChildren checks if the element is one of the first few children of body.
// Skip links are always placed at the very beginning of <body>.
func isFirstFewChildren(el *Element) bool {
	// Walk up to find body
	node := el.Parent
	for node != nil {
		if pel := nodeToElement(node); pel != nil && pel.TagName == "BODY" {
			// Check if el is within the first 5 children
			for i, child := range node.Children {
				if i >= 5 { return false }
				if child == &el.Node { return true }
				// Also check if el is nested 1 level deep in a wrapper div
				if cel := nodeToElement(child); cel != nil {
					for _, grandchild := range child.Children {
						if grandchild == &el.Node { return true }
					}
				}
			}
			return false
		}
		node = node.Parent
	}
	return false
}

// isNavLikeList checks if a UL/OL looks like a navigation menu.
// True if most LI children contain an <a> link as first child.
func isNavLikeList(el *Element) bool {
	liCount := 0
	linkLiCount := 0
	for _, child := range el.Children {
		if cel := nodeToElement(child); cel != nil && cel.TagName == "LI" {
			liCount++
			for _, gc := range child.Children {
				if gcel := nodeToElement(gc); gcel != nil && gcel.TagName == "A" {
					linkLiCount++
					break
				}
			}
		}
	}
	return liCount > 0 && linkLiCount*2 >= liCount // majority have links
}

// isParentNavLike checks if the LI's parent UL is a nav-like list.
func isParentNavLike(el *Element) bool {
	if el.Parent == nil { return false }
	if pel := nodeToElement(el.Parent); pel != nil && (pel.TagName == "UL" || pel.TagName == "OL") {
		return isNavLikeList(pel)
	}
	return false
}

// isInsideTag checks if the element is a descendant of a specific tag.
func isInsideTag(el *Element, tag string) bool {
	parent := el.Parent
	for parent != nil {
		if pel := nodeToElement(parent); pel != nil && pel.TagName == tag {
			return true
		}
		parent = parent.Parent
	}
	return false
}

func parseInlineStyle(style string, s *BoxStyle) {
	parts := strings.Split(style, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		colonIdx := strings.Index(part, ":")
		if colonIdx < 0 {
			continue
		}
		prop := strings.TrimSpace(part[:colonIdx])
		val := strings.TrimSpace(part[colonIdx+1:])

		switch prop {
		case "display":
			if val == "none" {
				s.Hidden = true
				s.Display = "none"
			} else if val == "flex" {
				s.Display = "flex"
				if s.FlexDirection == "" { s.FlexDirection = "row" }
				if s.AlignItems == "" { s.AlignItems = "stretch" }
				if s.JustifyContent == "" { s.JustifyContent = "flex-start" }
				if s.FlexWrap == "" { s.FlexWrap = "nowrap" }
			} else {
				s.Display = val
			}
		case "flex-direction":
			s.FlexDirection = val
		case "justify-content":
			s.JustifyContent = val
		case "align-items":
			s.AlignItems = val
		case "flex-wrap":
			s.FlexWrap = val
		case "gap":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.Gap = n
			}
		case "flex-grow":
			if n, err := strconv.ParseFloat(val, 64); err == nil {
				s.FlexGrow = n
			}
		case "flex":
			// shorthand: flex: 1 → flex-grow: 1
			if n, err := strconv.ParseFloat(strings.Fields(val)[0], 64); err == nil {
				s.FlexGrow = n
			}
		case "color":
			s.Color = parseColor(val)
		case "background-color", "background":
			s.BGColor = parseColor(val)
		case "font-size":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.FontSize = n
			}
		case "font-weight":
			s.Bold = val == "bold" || val == "700" || val == "800" || val == "900"
		case "padding":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.PaddingT, s.PaddingR, s.PaddingB, s.PaddingL = n, n, n, n
			}
		case "margin":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.MarginT, s.MarginB = n, n
			}
		case "margin-top":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.MarginT = n
			}
		case "margin-bottom":
			if n, err := strconv.Atoi(strings.TrimSuffix(val, "px")); err == nil {
				s.MarginB = n
			}
		}
	}
}

func parseColor(val string) Color {
	val = strings.TrimSpace(val)
	switch strings.ToLower(val) {
	case "white": return colorWhite
	case "black": return colorBlack
	case "red": return Color{220, 53, 69, 255}
	case "green": return Color{40, 167, 69, 255}
	case "blue": return Color{0, 123, 255, 255}
	case "gray", "grey": return colorGray
	case "transparent": return Color{0, 0, 0, 0}
	}
	if len(val) == 7 && val[0] == '#' {
		r, _ := strconv.ParseUint(val[1:3], 16, 8)
		g, _ := strconv.ParseUint(val[3:5], 16, 8)
		b, _ := strconv.ParseUint(val[5:7], 16, 8)
		return Color{uint8(r), uint8(g), uint8(b), 255}
	}
	if len(val) == 4 && val[0] == '#' {
		r, _ := strconv.ParseUint(string(val[1])+string(val[1]), 16, 8)
		g, _ := strconv.ParseUint(string(val[2])+string(val[2]), 16, 8)
		b, _ := strconv.ParseUint(string(val[3])+string(val[3]), 16, 8)
		return Color{uint8(r), uint8(g), uint8(b), 255}
	}
	return colorBlack
}
