package crema

import (
	"strings"

	"golang.org/x/net/html"
)

// ParseHTML parses an HTML string into a crema Document.
func ParseHTML(rawHTML string) *Document {
	doc := NewDocument()
	RegisterNode(&doc.Node, doc)

	parsed, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return doc
	}

	// Walk the parsed tree and build crema DOM
	convertNode(parsed, &doc.Node, doc)

	// Extract title
	if titleEl := findChildElement(&doc.Node, "TITLE"); titleEl != nil {
		var sb strings.Builder
		CollectTextFromElement(titleEl, &sb)
		doc.Title = sb.String()
	}

	return doc
}

func convertNode(src *html.Node, parent *Node, doc *Document) {
	for c := src.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.ElementNode:
			el := NewElement(c.Data)
			el.OwnerDoc = doc
			for _, attr := range c.Attr {
				el.SetAttribute(attr.Key, attr.Val)
			}
			RegisterNode(&el.Node, el)
			parent.AppendChild(&el.Node)
			convertNode(c, &el.Node, doc)

		case html.TextNode:
			data := c.Data
			// Skip whitespace-only text nodes
			if strings.TrimSpace(data) == "" {
				continue
			}
			tn := NewTextNode(data)
			tn.OwnerDoc = doc
			RegisterNode(&tn.Node, tn)
			parent.AppendChild(&tn.Node)

		case html.CommentNode:
			// Skip comments for now

		case html.DocumentNode:
			convertNode(c, parent, doc)
		}
	}
}

// ExtractScripts finds all <script> elements and returns their content.
// Inline scripts return the text content, external scripts return the src URL.
type Script struct {
	Inline bool
	Src    string
	Code   string
}

func ExtractScripts(doc *Document) []Script {
	var scripts []Script
	findScripts(&doc.Node, &scripts)
	return scripts
}

func findScripts(n *Node, scripts *[]Script) {
	if el := nodeToElement(n); el != nil && el.TagName == "SCRIPT" {
		src := el.GetAttribute("src")
		if src != "" {
			*scripts = append(*scripts, Script{Src: src})
		} else {
			var sb strings.Builder
			// Script content is in text children (raw, not escaped)
			for _, child := range n.Children {
				if tn := nodeToText(child); tn != nil {
					sb.WriteString(tn.Data)
				}
			}
			code := sb.String()
			if strings.TrimSpace(code) != "" {
				*scripts = append(*scripts, Script{Inline: true, Code: code})
			}
		}
	}
	for _, c := range n.Children {
		findScripts(c, scripts)
	}
}
