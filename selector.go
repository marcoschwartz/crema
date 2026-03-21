package crema

import (
	"strings"

	"github.com/marcoschwartz/espresso"
)

// QuerySelector returns the first element matching a CSS selector.
func QuerySelector(root *Node, selector string) *Element {
	results := queryAll(root, selector, true)
	if len(results) > 0 {
		return results[0]
	}
	return nil
}

// QuerySelectorAll returns all elements matching a CSS selector.
func QuerySelectorAll(root *Node, selector string) []*Element {
	return queryAll(root, selector, false)
}

func queryAll(root *Node, selector string, firstOnly bool) []*Element {
	parts := parseSelector(selector)
	var results []*Element
	matchDescendants(root, parts, &results, firstOnly)
	return results
}

// ─── Selector parsing ───────────────────────────────────────

type selectorPart struct {
	tag     string   // e.g. "div", "*", ""
	id      string   // e.g. "main"
	classes []string // e.g. ["active", "visible"]
	attrs   []attrSelector
}

type attrSelector struct {
	name string
	op   string // "=", "~=", "^=", "$=", "*=", "" (just has attr)
	val  string
}

func parseSelector(sel string) [][]selectorPart {
	// Split by comma for multiple selectors
	groups := strings.Split(sel, ",")
	var result [][]selectorPart
	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		// Split by space for descendant combinator
		// Simple approach: split by whitespace
		tokens := strings.Fields(group)
		var chain []selectorPart
		for _, tok := range tokens {
			// Handle > (child combinator) — skip for now, treat as descendant
			if tok == ">" {
				continue
			}
			chain = append(chain, parseSingleSelector(tok))
		}
		if len(chain) > 0 {
			result = append(result, chain)
		}
	}
	return result
}

func parseSingleSelector(sel string) selectorPart {
	var p selectorPart
	i := 0

	// Tag name
	start := i
	for i < len(sel) && sel[i] != '.' && sel[i] != '#' && sel[i] != '[' && sel[i] != ':' {
		i++
	}
	if i > start {
		p.tag = strings.ToUpper(sel[start:i])
	}

	// Parse remaining segments
	for i < len(sel) {
		switch sel[i] {
		case '#':
			i++
			start = i
			for i < len(sel) && sel[i] != '.' && sel[i] != '#' && sel[i] != '[' && sel[i] != ':' {
				i++
			}
			p.id = sel[start:i]
		case '.':
			i++
			start = i
			for i < len(sel) && sel[i] != '.' && sel[i] != '#' && sel[i] != '[' && sel[i] != ':' {
				i++
			}
			p.classes = append(p.classes, sel[start:i])
		case '[':
			i++
			start = i
			for i < len(sel) && sel[i] != ']' {
				i++
			}
			attrStr := sel[start:i]
			if i < len(sel) {
				i++ // skip ]
			}
			p.attrs = append(p.attrs, parseAttrSelector(attrStr))
		case ':':
			// Skip pseudo-classes for now
			i++
			for i < len(sel) && sel[i] != '.' && sel[i] != '#' && sel[i] != '[' && sel[i] != ':' && sel[i] != '(' {
				i++
			}
			if i < len(sel) && sel[i] == '(' {
				depth := 1
				i++
				for i < len(sel) && depth > 0 {
					if sel[i] == '(' { depth++ }
					if sel[i] == ')' { depth-- }
					i++
				}
			}
		default:
			i++
		}
	}
	return p
}

func parseAttrSelector(s string) attrSelector {
	for _, op := range []string{"~=", "^=", "$=", "*=", "="} {
		idx := strings.Index(s, op)
		if idx >= 0 {
			name := strings.TrimSpace(s[:idx])
			val := strings.TrimSpace(s[idx+len(op):])
			val = strings.Trim(val, `"'`)
			return attrSelector{name: name, op: op, val: val}
		}
	}
	return attrSelector{name: strings.TrimSpace(s)}
}

// ─── Matching ───────────────────────────────────────────────

func matchDescendants(n *Node, selectorGroups [][]selectorPart, results *[]*Element, firstOnly bool) {
	for _, c := range n.Children {
		if firstOnly && len(*results) > 0 {
			return
		}
		if el := nodeToElement(c); el != nil {
			for _, chain := range selectorGroups {
				if matchChain(el, chain) {
					*results = append(*results, el)
					if firstOnly {
						return
					}
					break
				}
			}
		}
		matchDescendants(c, selectorGroups, results, firstOnly)
	}
}

func matchChain(el *Element, chain []selectorPart) bool {
	if len(chain) == 0 {
		return false
	}
	// Last part must match the element itself
	if !matchesPart(el, chain[len(chain)-1]) {
		return false
	}
	if len(chain) == 1 {
		return true
	}
	// Walk up ancestors for remaining parts (descendant combinator)
	remaining := chain[:len(chain)-1]
	ancestor := el.Parent
	ri := len(remaining) - 1
	for ancestor != nil && ri >= 0 {
		if ael := nodeToElement(ancestor); ael != nil {
			if matchesPart(ael, remaining[ri]) {
				ri--
			}
		}
		ancestor = ancestor.Parent
	}
	return ri < 0
}

func matchesPart(el *Element, p selectorPart) bool {
	// Tag
	if p.tag != "" && p.tag != "*" && el.TagName != p.tag {
		return false
	}
	// ID
	if p.id != "" && el.ID != p.id {
		return false
	}
	// Classes
	for _, cls := range p.classes {
		found := false
		for _, c := range el.ClassList {
			if c == cls {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	// Attributes
	for _, attr := range p.attrs {
		val, exists := el.Attrs[attr.name]
		if !exists {
			return false
		}
		switch attr.op {
		case "=":
			if val != attr.val { return false }
		case "~=":
			found := false
			for _, w := range strings.Fields(val) {
				if w == attr.val { found = true; break }
			}
			if !found { return false }
		case "^=":
			if !strings.HasPrefix(val, attr.val) { return false }
		case "$=":
			if !strings.HasSuffix(val, attr.val) { return false }
		case "*=":
			if !strings.Contains(val, attr.val) { return false }
		}
	}
	return true
}

// ─── Register querySelector/querySelectorAll on document + elements ─

func AddQuerySelectors(docJS *espresso.Value, doc *Document) {
	docJS.Object()["querySelector"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		el := QuerySelector(&doc.Node, args[0].String())
		if el != nil { return elementToJS(el) }
		return espresso.Null
	})
	docJS.Object()["querySelectorAll"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewArr(nil) }
		elements := QuerySelectorAll(&doc.Node, args[0].String())
		arr := make([]*espresso.Value, len(elements))
		for i, el := range elements {
			arr[i] = elementToJS(el)
		}
		return espresso.NewArr(arr)
	})
}

func AddQuerySelectorsToElement(elJS *espresso.Value, el *Element) {
	elJS.Object()["querySelector"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		found := QuerySelector(&el.Node, args[0].String())
		if found != nil { return elementToJS(found) }
		return espresso.Null
	})
	elJS.Object()["querySelectorAll"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewArr(nil) }
		elements := QuerySelectorAll(&el.Node, args[0].String())
		arr := make([]*espresso.Value, len(elements))
		for i, el := range elements {
			arr[i] = elementToJS(el)
		}
		return espresso.NewArr(arr)
	})
}
