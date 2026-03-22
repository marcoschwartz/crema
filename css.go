package crema

import (
	"strings"
)

// CSSRules holds parsed CSS rules that affect visibility and display.
type CSSRules struct {
	HiddenSelectors []string // selectors with display:none or visibility:hidden
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
			// Split comma-separated selectors
			for _, sel := range strings.Split(selector, ",") {
				sel = strings.TrimSpace(sel)
				if sel != "" {
					rules.HiddenSelectors = append(rules.HiddenSelectors, sel)
				}
			}
		}

		i = i + braceStart + braceEnd + 1
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
