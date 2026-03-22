package crema

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// CSSRule holds a parsed CSS rule with selector and properties.
type CSSRule struct {
	Selector string
	Props    map[string]string
}

// CSSRules holds parsed CSS rules that affect visibility and display.
type CSSRules struct {
	HiddenSelectors []string   // selectors with display:none or visibility:hidden
	StyleRules      []CSSRule  // selectors with other style properties
}

// ParseStyleTags extracts CSS rules from <style> tags in the document.
func ParseStyleTags(doc *Document) *CSSRules {
	rules := &CSSRules{}
	findStyleTags(&doc.Node, rules)
	return rules
}

func findStyleTags(n *Node, rules *CSSRules) {
	if el := nodeToElement(n); el != nil && el.TagName == "STYLE" {
		// Get CSS text from children
		var sb strings.Builder
		for _, child := range n.Children {
			if tn := nodeToText(child); tn != nil {
				sb.WriteString(tn.Data)
			}
		}
		css := sb.String()
		parseCSS(css, rules)
	}
	for _, child := range n.Children {
		findStyleTags(child, rules)
	}
}

// parseCSS extracts selectors with display:none or visibility:hidden.
func parseCSS(css string, rules *CSSRules) {
	// Remove comments
	for {
		start := strings.Index(css, "/*")
		if start < 0 { break }
		end := strings.Index(css[start:], "*/")
		if end < 0 { break }
		css = css[:start] + css[start+end+2:]
	}

	i := 0
	for i < len(css) {
		// Find selector (everything before {)
		braceStart := strings.Index(css[i:], "{")
		if braceStart < 0 { break }
		selector := strings.TrimSpace(css[i : i+braceStart])

		// Find matching }
		braceEnd := strings.Index(css[i+braceStart:], "}")
		if braceEnd < 0 { break }
		body := css[i+braceStart+1 : i+braceStart+braceEnd]

		// Handle @rules
		if strings.HasPrefix(selector, "@") {
			if strings.HasPrefix(selector, "@media") {
				// Parse media query — check if it applies to our viewport (1280px desktop)
				if mediaApplies(selector, 1280) {
					// The body contains nested rules — parse them recursively
					// But body might contain nested { }, so find the matching }
					innerCSS := findMatchingBraceContent(css[i+braceStart:])
					if innerCSS != "" {
						parseCSS(innerCSS, rules)
					}
				}
				// Skip past the entire @media block
				depth := 0
				for j := i + braceStart; j < len(css); j++ {
					if css[j] == '{' { depth++ }
					if css[j] == '}' { depth--; if depth == 0 { i = j + 1; break } }
				}
				continue
			}
			// Skip other @rules (keyframes, font-face, etc.)
			depth := 0
			for j := i + braceStart; j < len(css); j++ {
				if css[j] == '{' { depth++ }
				if css[j] == '}' { depth--; if depth == 0 { i = j + 1; break } }
			}
			continue
		}

		// Check if body contains display:none or visibility:hidden
		bodyLower := strings.ToLower(body)
		isHidden := strings.Contains(bodyLower, "display:none") ||
			strings.Contains(bodyLower, "display: none") ||
			strings.Contains(bodyLower, "visibility:hidden") ||
			strings.Contains(bodyLower, "visibility: hidden")

		if isHidden {
			for _, sel := range strings.Split(selector, ",") {
				sel = strings.TrimSpace(sel)
				if sel != "" {
					rules.HiddenSelectors = append(rules.HiddenSelectors, sel)
				}
			}
		}

		// Also extract color/background/font-size rules for styling
		props := parseStyleProps(body)
		if len(props) > 0 {
			for _, sel := range strings.Split(selector, ",") {
				sel = strings.TrimSpace(sel)
				if sel != "" {
					rules.StyleRules = append(rules.StyleRules, CSSRule{Selector: sel, Props: props})
				}
			}
		}

		i = i + braceStart + braceEnd + 1
	}
}

// ParseExternalCSS fetches external stylesheets and extracts rules.
func ParseExternalCSS(doc *Document, pageURL string, client *http.Client) *CSSRules {
	rules := ParseStyleTags(doc)
	// Find <link rel="stylesheet"> elements
	findLinkTags(&doc.Node, rules, pageURL, client)
	return rules
}

func findLinkTags(n *Node, rules *CSSRules, pageURL string, client *http.Client) {
	if el := nodeToElement(n); el != nil && el.TagName == "LINK" {
		rel := strings.ToLower(el.GetAttribute("rel"))
		if rel == "stylesheet" {
			href := el.GetAttribute("href")
			if href != "" {
				fetchAndParseCSS(href, pageURL, client, rules)
			}
		}
	}
	for _, child := range n.Children {
		findLinkTags(child, rules, pageURL, client)
	}
}

func fetchAndParseCSS(href, pageURL string, client *http.Client, rules *CSSRules) {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	} else if strings.HasPrefix(href, "/") && pageURL != "" {
		idx := strings.Index(pageURL, "://")
		if idx > 0 {
			rest := pageURL[idx+3:]
			if si := strings.Index(rest, "/"); si > 0 {
				href = pageURL[:idx+3+si] + href
			}
		}
	}
	if !strings.HasPrefix(href, "http") { return }

	// Skip known non-essential CSS (fonts, icons)
	lower := strings.ToLower(href)
	for _, skip := range []string{"fonts.googleapis.com", "font-awesome", "fontawesome", "icons"} {
		if strings.Contains(lower, skip) { return }
	}

	if client == nil { return }

	req, err := http.NewRequest("GET", href, nil)
	if err != nil { return }
	req.Header.Set("Accept", "text/css")

	resp, err := client.Do(req)
	if err != nil { return }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return }

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024)) // 1MB limit
	if err != nil { return }

	parseCSS(string(body), rules)
}

// mediaApplies checks if a @media query applies to the given viewport width.
// Handles: @media (min-width: Xpx), @media (max-width: Xpx), @media screen
func mediaApplies(query string, viewportW int) bool {
	query = strings.ToLower(query)

	// Always match: @media screen, @media all
	if !strings.Contains(query, "min-width") && !strings.Contains(query, "max-width") {
		// No width constraint — @media screen, @media print, etc.
		if strings.Contains(query, "print") { return false }
		return true
	}

	// Check min-width
	if strings.Contains(query, "min-width") {
		n := 0
		// Extract the number after "min-width:"
		idx := strings.Index(query, "min-width")
		if idx >= 0 {
			rest := query[idx+9:] // skip "min-width"
			rest = strings.TrimLeft(rest, ": ")
			fmt.Sscanf(rest, "%dpx", &n)
			if n == 0 { fmt.Sscanf(rest, "%d", &n) }
			if n > 0 && viewportW < n {
				return false // viewport too narrow
			}
		}
	}

	// Check max-width
	if strings.Contains(query, "max-width") {
		n := 0
		idx := strings.Index(query, "max-width")
		if idx >= 0 {
			rest := query[idx+9:]
			rest = strings.TrimLeft(rest, ": ")
			fmt.Sscanf(rest, "%dpx", &n)
			if n == 0 { fmt.Sscanf(rest, "%d", &n) }
			if n > 0 && viewportW > n {
				return false // viewport too wide
			}
		}
	}

	return true
}

// findMatchingBraceContent returns the content between the first { and its matching }.
func findMatchingBraceContent(s string) string {
	start := strings.Index(s, "{")
	if start < 0 { return "" }
	depth := 0
	for i := start; i < len(s); i++ {
		if s[i] == '{' { depth++ }
		if s[i] == '}' {
			depth--
			if depth == 0 {
				return s[start+1 : i]
			}
		}
	}
	return ""
}

// parseStyleProps extracts interesting CSS properties from a rule body.
func parseStyleProps(body string) map[string]string {
	props := map[string]string{}
	for _, decl := range strings.Split(body, ";") {
		decl = strings.TrimSpace(decl)
		colonIdx := strings.Index(decl, ":")
		if colonIdx < 0 { continue }
		prop := strings.TrimSpace(decl[:colonIdx])
		val := strings.TrimSpace(decl[colonIdx+1:])
		// Only keep properties we can use
		switch prop {
		case "color", "background-color", "background", "background-image",
			"font-size", "font-weight", "font-style", "font-family",
			"display", "visibility", "position",
			"padding", "padding-top", "padding-bottom", "padding-left", "padding-right",
			"margin", "margin-top", "margin-bottom", "margin-left", "margin-right",
			"border", "border-color", "border-radius",
			"text-decoration", "text-align",
			"flex-direction", "justify-content", "align-items", "gap", "flex-wrap", "flex",
			"width", "max-width", "min-width", "height", "min-height",
			"overflow", "float":
			props[prop] = val
		}
	}
	return props
}

// ApplyCSS applies CSS style rules to an element's BoxStyle.
func (rules *CSSRules) ApplyCSS(el *Element, s *BoxStyle) {
	if rules == nil { return }
	for _, rule := range rules.StyleRules {
		parts := parseSelector(rule.Selector)
		matched := false
		for _, chain := range parts {
			if matchChain(el, chain) { matched = true; break }
		}
		if !matched { continue }
		applyCSSProps(rule.Props, s)
	}
}

func applyCSSProps(props map[string]string, s *BoxStyle) {
	for prop, val := range props {
		switch prop {
		case "color":
			s.Color = parseColor(val)
		case "background-color":
			s.BGColor = parseColor(val)
		case "background":
			if !strings.Contains(val, "url(") && !strings.Contains(val, "gradient") {
				s.BGColor = parseColor(val)
			}
		case "font-size":
			n := parsePx(val)
			if n == 0 && (strings.HasSuffix(val, "em") || strings.HasSuffix(val, "rem")) {
				var f float64
				fmt.Sscanf(val, "%f", &f)
				n = int(f * 16)
			}
			if n > 0 && n < 72 { s.FontSize = n }
		case "font-weight":
			s.Bold = val == "bold" || val == "700" || val == "800" || val == "900"
		case "font-family":
			// We can't switch fonts, but we can detect serif vs sans-serif
			// and adjust spacing slightly. For now, just note it.
		case "font-style":
			s.Italic = val == "italic"
		case "display":
			switch val {
			case "none":
				s.Hidden = true; s.Display = "none"
			case "flex":
				s.Display = "flex"
				if s.FlexDirection == "" { s.FlexDirection = "row" }
				if s.AlignItems == "" { s.AlignItems = "stretch" }
			case "grid":
				s.Display = "flex"; s.FlexDirection = "row"; s.FlexWrap = "wrap"
			case "inline", "inline-block", "inline-flex":
				s.Display = "inline"
			case "block":
				s.Display = "block"
			}
		case "visibility":
			if val == "hidden" { s.Hidden = true; s.Display = "none" }
		case "position":
			// position:fixed — only hide if it's likely a cookie banner/overlay
			// Keep navbars visible (they're at the top and contain navigation)
			if val == "fixed" {
				// Don't hide — render in normal flow. The element might be a navbar.
				// Cookie banners are usually at the bottom with specific classes.
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
			if n := parsePx(val); n > 0 { s.Gap = n }
		case "flex":
			fields := strings.Fields(val)
			if len(fields) > 0 {
				var f float64
				fmt.Sscanf(fields[0], "%f", &f)
				if f > 0 { s.FlexGrow = f }
			}
		case "padding":
			parts := strings.Fields(val)
			switch len(parts) {
			case 1:
				if n := parsePx(parts[0]); n >= 0 { s.PaddingT, s.PaddingR, s.PaddingB, s.PaddingL = n, n, n, n }
			case 2:
				v := parsePx(parts[0]); h := parsePx(parts[1])
				s.PaddingT, s.PaddingB = v, v; s.PaddingL, s.PaddingR = h, h
			case 3:
				s.PaddingT = parsePx(parts[0]); s.PaddingR = parsePx(parts[1]); s.PaddingB = parsePx(parts[2]); s.PaddingL = parsePx(parts[1])
			case 4:
				s.PaddingT = parsePx(parts[0]); s.PaddingR = parsePx(parts[1]); s.PaddingB = parsePx(parts[2]); s.PaddingL = parsePx(parts[3])
			}
		case "padding-top":
			if n := parsePx(val); n > 0 { s.PaddingT = n }
		case "padding-bottom":
			if n := parsePx(val); n > 0 { s.PaddingB = n }
		case "padding-left":
			if n := parsePx(val); n > 0 { s.PaddingL = n }
		case "padding-right":
			if n := parsePx(val); n > 0 { s.PaddingR = n }
		case "margin":
			if strings.Contains(val, "auto") {
				// margin: X auto → center horizontally
			} else {
				parts := strings.Fields(val)
				switch len(parts) {
				case 1:
					if n := parsePx(parts[0]); n >= 0 { s.MarginT, s.MarginB = n, n }
				case 2:
					s.MarginT = parsePx(parts[0]); s.MarginB = parsePx(parts[0])
				case 4:
					s.MarginT = parsePx(parts[0]); s.MarginB = parsePx(parts[2])
					s.MarginR = parsePx(parts[1]); s.MarginL = parsePx(parts[3])
				}
			}
		case "margin-top":
			if n := parsePx(val); n >= 0 { s.MarginT = n }
		case "margin-bottom":
			if n := parsePx(val); n >= 0 { s.MarginB = n }
		case "margin-left":
			if n := parsePx(val); n >= 0 { s.MarginL = n }
		case "margin-right":
			if n := parsePx(val); n >= 0 { s.MarginR = n }
		case "max-width":
			// max-width handled via layout
		case "float":
			if val == "left" || val == "right" {
				// float:left — keep as block. The parent will become flex
				// if it detects float children. Don't set inline — that
				// would bypass the flex layout path.
			}
		case "width":
			if strings.HasSuffix(val, "%") {
				pct := 0
				// Handle decimal percentages like 50%, 33.33%, 16.666%
				var fpct float64
				fmt.Sscanf(val, "%f", &fpct)
				pct = int(fpct)
				if pct > 0 && pct <= 100 {
					s.WidthPct = pct
					s.FlexGrow = fpct / 100.0
				}
			}
		case "text-decoration":
			s.Underline = strings.Contains(val, "underline")
		case "text-align":
			// stored but not used for layout yet
		case "border-radius":
			// stored but not used for rendering yet
		}
	}
}

func parsePx(val string) int {
	val = strings.TrimSpace(val)
	val = strings.TrimSuffix(val, "px")
	val = strings.TrimSuffix(val, "!important")
	val = strings.TrimSpace(val)
	n := 0
	fmt.Sscanf(val, "%d", &n)
	return n
}

// IsHiddenByCSS checks if an element matches any CSS hidden selector.
func (rules *CSSRules) IsHiddenByCSS(el *Element) bool {
	if rules == nil { return false }
	for _, sel := range rules.HiddenSelectors {
		parts := parseSelector(sel)
		for _, chain := range parts {
			if matchChain(el, chain) {
				return true
			}
		}
	}
	return false
}
