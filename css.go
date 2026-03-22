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

		// Skip @rules (media queries, keyframes, etc.)
		if strings.HasPrefix(selector, "@") {
			// Find the end of the @rule block (may be nested)
			i = i + braceStart + braceEnd + 1
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
		case "color", "background-color", "background",
			"font-size", "font-weight", "font-style",
			"display", "visibility",
			"padding", "padding-top", "padding-bottom", "padding-left", "padding-right",
			"margin", "margin-top", "margin-bottom",
			"border", "border-color",
			"text-decoration", "text-align",
			"flex-direction", "justify-content", "align-items", "gap", "flex-wrap":
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
		// Apply properties
		for prop, val := range rule.Props {
			switch prop {
			case "color":
				s.Color = parseColor(val)
			case "background-color", "background":
				if !strings.Contains(val, "url(") && !strings.Contains(val, "gradient") {
					s.BGColor = parseColor(val)
				}
			case "font-size":
				n := 0
				if strings.HasSuffix(val, "px") {
					fmt.Sscanf(val, "%dpx", &n)
				} else if strings.HasSuffix(val, "em") || strings.HasSuffix(val, "rem") {
					var f float64
					fmt.Sscanf(val, "%f", &f)
					n = int(f * 16)
				}
				if n > 0 && n < 72 { s.FontSize = n }
			case "font-weight":
				s.Bold = val == "bold" || val == "700" || val == "800" || val == "900"
			case "display":
				if val == "none" { s.Hidden = true; s.Display = "none" }
				if val == "flex" { s.Display = "flex"; s.FlexDirection = "row" }
				if val == "grid" { s.Display = "flex"; s.FlexDirection = "row"; s.FlexWrap = "wrap" }
			case "gap":
				n := 0
				fmt.Sscanf(val, "%dpx", &n)
				if n > 0 { s.Gap = n }
			}
		}
	}
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
