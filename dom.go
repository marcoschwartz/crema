package crema

import (
	"strings"

	"github.com/marcoschwartz/espresso"
)

// activeVM holds a reference to the current page's VM for event dispatching.
// Set during setupJS. Used by dispatchEvent to call JS callbacks with proper scope.
var activeVM *espresso.VM

// dispatchCallback calls a JS event listener callback with proper scope.
func dispatchCallback(cb *espresso.Value, eventObj *espresso.Value, index int) {
	if activeVM != nil {
		scope := activeVM.Scope()
		espresso.CallFunc(scope, cb, map[string]*espresso.Value{"event": eventObj})
		// Merge scope changes back
		for k, v := range scope {
			activeVM.SetValue(k, v)
		}
	} else {
		espresso.CallFunc(nil, cb, map[string]*espresso.Value{"event": eventObj})
	}
}

// ─── DOM Node Types ─────────────────────────────────────────

const (
	NodeElement  = 1
	NodeText     = 3
	NodeComment  = 8
	NodeDocument = 9
)

// Node is the base DOM node.
type Node struct {
	NodeType   int
	NodeName   string
	Parent     *Node
	Children   []*Node
	OwnerDoc   *Document
	jsValue    *espresso.Value // cached JS representation
}

// Element is an HTML element node.
type Element struct {
	Node
	TagName    string
	Attrs      map[string]string
	ClassList  []string
	ID         string
	Listeners  map[string][]*espresso.Value
	StyleMap   map[string]string
}

// TextNode is a text content node.
type TextNode struct {
	Node
	Data string
}

// Document is the root document node.
type Document struct {
	Node
	Title string
}

// ─── Tree operations ────────────────────────────────────────

func (n *Node) AppendChild(child *Node) {
	child.Parent = n
	n.Children = append(n.Children, child)
}

func (n *Node) RemoveChild(child *Node) {
	for i, c := range n.Children {
		if c == child {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			child.Parent = nil
			return
		}
	}
}

func (n *Node) FirstChild() *Node {
	if len(n.Children) > 0 {
		return n.Children[0]
	}
	return nil
}

func (n *Node) LastChild() *Node {
	if len(n.Children) > 0 {
		return n.Children[len(n.Children)-1]
	}
	return nil
}

func (n *Node) NextSibling() *Node {
	if n.Parent == nil {
		return nil
	}
	for i, c := range n.Parent.Children {
		if c == n && i+1 < len(n.Parent.Children) {
			return n.Parent.Children[i+1]
		}
	}
	return nil
}

func (n *Node) PreviousSibling() *Node {
	if n.Parent == nil {
		return nil
	}
	for i, c := range n.Parent.Children {
		if c == n && i > 0 {
			return n.Parent.Children[i-1]
		}
	}
	return nil
}

// TextContent returns the text content of the node and its descendants.
func (n *Node) TextContent() string {
	if n.NodeType == NodeText {
		// Caller should cast to TextNode
		return ""
	}
	var sb strings.Builder
	collectText(n, &sb)
	return sb.String()
}

func collectText(n *Node, sb *strings.Builder) {
	for _, child := range n.Children {
		if child.NodeType == NodeText {
			// Find the TextNode data via the jsValue or direct cast
			if tn, ok := child.nodeData().(*TextNode); ok {
				sb.WriteString(tn.Data)
			}
		} else {
			collectText(child, sb)
		}
	}
}

// nodeData returns the concrete node type if embedded.
func (n *Node) nodeData() interface{} {
	return nil // overridden by wrapper
}

// InnerHTML serializes children to HTML string.
func (el *Element) InnerHTML() string {
	var sb strings.Builder
	for _, child := range el.Children {
		serializeNode(child, &sb)
	}
	return sb.String()
}

// OuterHTML serializes the element and its children.
func (el *Element) OuterHTML() string {
	var sb strings.Builder
	serializeElement(el, &sb)
	return sb.String()
}

func serializeNode(n *Node, sb *strings.Builder) {
	switch n.NodeType {
	case NodeElement:
		if el := nodeToElement(n); el != nil {
			serializeElement(el, sb)
		}
	case NodeText:
		if tn := nodeToText(n); tn != nil {
			sb.WriteString(tn.Data)
		}
	case NodeComment:
		sb.WriteString("<!--")
		sb.WriteString("-->")
	}
}

func serializeElement(el *Element, sb *strings.Builder) {
	sb.WriteByte('<')
	sb.WriteString(strings.ToLower(el.TagName))
	for k, v := range el.Attrs {
		sb.WriteByte(' ')
		sb.WriteString(k)
		sb.WriteString(`="`)
		sb.WriteString(v)
		sb.WriteByte('"')
	}
	sb.WriteByte('>')
	// Void elements
	switch strings.ToLower(el.TagName) {
	case "br", "hr", "img", "input", "meta", "link", "area", "base", "col", "embed", "source", "track", "wbr":
		return
	}
	for _, child := range el.Children {
		serializeNode(child, sb)
	}
	sb.WriteString("</")
	sb.WriteString(strings.ToLower(el.TagName))
	sb.WriteByte('>')
}

// GetAttribute returns an attribute value.
func (el *Element) GetAttribute(name string) string {
	if el.Attrs == nil {
		return ""
	}
	return el.Attrs[name]
}

// SetAttribute sets an attribute value.
func (el *Element) SetAttribute(name, value string) {
	if el.Attrs == nil {
		el.Attrs = make(map[string]string)
	}
	el.Attrs[name] = value
	if name == "class" {
		el.ClassList = strings.Fields(value)
	}
	if name == "id" {
		el.ID = value
	}
}

// HasAttribute checks if an attribute exists.
func (el *Element) HasAttribute(name string) bool {
	if el.Attrs == nil {
		return false
	}
	_, ok := el.Attrs[name]
	return ok
}

// RemoveAttribute removes an attribute.
func (el *Element) RemoveAttribute(name string) {
	delete(el.Attrs, name)
}

// ─── Constructor helpers ────────────────────────────────────

func NewDocument() *Document {
	doc := &Document{}
	doc.NodeType = NodeDocument
	doc.NodeName = "#document"
	doc.OwnerDoc = doc
	return doc
}

func NewElement(tag string) *Element {
	el := &Element{TagName: strings.ToUpper(tag)}
	el.NodeType = NodeElement
	el.NodeName = strings.ToUpper(tag)
	el.Attrs = make(map[string]string)
	return el
}

func NewTextNode(data string) *TextNode {
	tn := &TextNode{Data: data}
	tn.NodeType = NodeText
	tn.NodeName = "#text"
	return tn
}

// ─── Node ↔ Element/Text helpers ────────────────────────────

// We store the concrete type pointer in a map keyed by *Node.
var nodeRegistry = map[*Node]interface{}{}

func RegisterNode(n *Node, concrete interface{}) {
	nodeRegistry[n] = concrete
}

func nodeToElement(n *Node) *Element {
	if v, ok := nodeRegistry[n]; ok {
		if el, ok := v.(*Element); ok {
			return el
		}
	}
	return nil
}

func nodeToText(n *Node) *TextNode {
	if v, ok := nodeRegistry[n]; ok {
		if tn, ok := v.(*TextNode); ok {
			return tn
		}
	}
	return nil
}

// ─── JS binding ─────────────────────────────────────────────
// Exposes DOM nodes to espresso as JS objects with getters/setters
// and prototype chains.

// Prototypes shared across all nodes of the same type.
var (
	protoEventTarget *espresso.Value
	protoNode        *espresso.Value
	protoElement     *espresso.Value
	protoDocument    *espresso.Value
	protosBuilt      bool
)

func buildPrototypes() {
	if protosBuilt {
		return
	}
	protosBuilt = true

	// EventTarget prototype (root of chain)
	protoEventTarget = espresso.NewObj(map[string]*espresso.Value{})

	// Node prototype
	protoNode = espresso.NewObj(map[string]*espresso.Value{})
	protoNode.DefineGetter("nodeType", func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined // overridden per instance
	})

	// Element prototype
	protoElement = espresso.NewObj(map[string]*espresso.Value{})

	// Document prototype
	protoDocument = espresso.NewObj(map[string]*espresso.Value{})
}

// NodeToJS creates the espresso Value for a DOM node.
func NodeToJS(n *Node) *espresso.Value {
	if n == nil {
		return espresso.Null
	}
	if n.jsValue != nil {
		return n.jsValue
	}
	buildPrototypes()

	switch n.NodeType {
	case NodeElement:
		el := nodeToElement(n)
		if el != nil {
			return elementToJS(el)
		}
	case NodeText:
		tn := nodeToText(n)
		if tn != nil {
			return textToJS(tn)
		}
	case NodeDocument:
		// handled by DocumentToJS
	}

	// Fallback: generic node
	v := espresso.NewObj(map[string]*espresso.Value{
		"nodeType": espresso.NewNum(float64(n.NodeType)),
		"nodeName": espresso.NewStr(n.NodeName),
	})
	n.jsValue = v
	return v
}

func elementToJS(el *Element) *espresso.Value {
	if el.jsValue != nil {
		return el.jsValue
	}

	v := espresso.NewObj(map[string]*espresso.Value{
		"nodeType": espresso.NewNum(NodeElement),
		"nodeName": espresso.NewStr(el.NodeName),
		"tagName":  espresso.NewStr(el.TagName),
	})

	// ID
	v.DefineGetter("id", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(el.ID)
	})
	v.DefineSetter("id", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			el.ID = args[0].String()
			el.Attrs["id"] = el.ID
		}
		return espresso.Undefined
	})

	// className
	v.DefineGetter("className", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(strings.Join(el.ClassList, " "))
	})
	v.DefineSetter("className", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			val := args[0].String()
			el.ClassList = strings.Fields(val)
			el.Attrs["class"] = val
		}
		return espresso.Undefined
	})

	// textContent
	v.DefineGetter("textContent", func(args []*espresso.Value) *espresso.Value {
		var sb strings.Builder
		CollectTextFromElement(el, &sb)
		return espresso.NewStr(sb.String())
	})
	v.DefineSetter("textContent", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			el.Children = nil
			tn := NewTextNode(args[0].String())
			RegisterNode(&tn.Node, tn)
			el.AppendChild(&tn.Node)
		}
		return espresso.Undefined
	})

	// innerHTML
	v.DefineGetter("innerHTML", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(el.InnerHTML())
	})
	v.DefineSetter("innerHTML", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			el.Children = nil
			// Parse HTML and add children (simple text for now)
			tn := NewTextNode(args[0].String())
			RegisterNode(&tn.Node, tn)
			el.AppendChild(&tn.Node)
		}
		return espresso.Undefined
	})

	// outerHTML
	v.DefineGetter("outerHTML", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(el.OuterHTML())
	})

	// children / childNodes
	v.DefineGetter("children", func(args []*espresso.Value) *espresso.Value {
		arr := make([]*espresso.Value, 0)
		for _, c := range el.Children {
			if c.NodeType == NodeElement {
				arr = append(arr, NodeToJS(c))
			}
		}
		return espresso.NewArr(arr)
	})
	v.DefineGetter("childNodes", func(args []*espresso.Value) *espresso.Value {
		arr := make([]*espresso.Value, len(el.Children))
		for i, c := range el.Children {
			arr[i] = NodeToJS(c)
		}
		return espresso.NewArr(arr)
	})

	// parentNode / parentElement
	v.DefineGetter("parentNode", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(el.Parent)
	})
	v.DefineGetter("parentElement", func(args []*espresso.Value) *espresso.Value {
		if el.Parent != nil && el.Parent.NodeType == NodeElement {
			return NodeToJS(el.Parent)
		}
		return espresso.Null
	})

	// firstChild / lastChild / nextSibling / previousSibling
	v.DefineGetter("firstChild", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(el.FirstChild())
	})
	v.DefineGetter("lastChild", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(el.LastChild())
	})
	v.DefineGetter("nextSibling", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(el.NextSibling())
	})
	v.DefineGetter("previousSibling", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(el.PreviousSibling())
	})

	// Methods
	v.Object()["getAttribute"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			return espresso.Null
		}
		val := el.GetAttribute(args[0].String())
		if val == "" && !el.HasAttribute(args[0].String()) {
			return espresso.Null
		}
		return espresso.NewStr(val)
	})
	v.Object()["setAttribute"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) >= 2 {
			el.SetAttribute(args[0].String(), args[1].String())
		}
		return espresso.Undefined
	})
	v.Object()["hasAttribute"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			return espresso.NewBool(false)
		}
		return espresso.NewBool(el.HasAttribute(args[0].String()))
	})
	v.Object()["removeAttribute"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 {
			el.RemoveAttribute(args[0].String())
		}
		return espresso.Undefined
	})
	v.Object()["appendChild"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		// TODO: extract Node from JS value and append
		return espresso.Undefined
	})

	// addEventListener / removeEventListener / dispatchEvent
	if el.Listeners == nil {
		el.Listeners = make(map[string][]*espresso.Value)
	}
	v.Object()["addEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		el.Listeners[event] = append(el.Listeners[event], cb)
		return espresso.Undefined
	})
	v.Object()["removeEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		listeners := el.Listeners[event]
		for i, l := range listeners {
			if l == cb {
				el.Listeners[event] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		return espresso.Undefined
	})
	v.Object()["dispatchEvent"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 1 {
			return espresso.NewBool(false)
		}
		eventObj := args[0]
		eventType := ""
		if eventObj.IsObject() {
			t := eventObj.Get("type")
			if t != nil && !t.IsUndefined() {
				eventType = t.String()
			}
		} else {
			eventType = eventObj.String()
		}
		for i, cb := range el.Listeners[eventType] {
			dispatchCallback(cb, eventObj, i)
		}
		return espresso.NewBool(true)
	})

	// style object
	if el.StyleMap == nil {
		el.StyleMap = make(map[string]string)
	}
	styleObj := espresso.NewObj(map[string]*espresso.Value{})
	styleProps := []string{"display", "visibility", "color", "backgroundColor", "fontSize",
		"width", "height", "margin", "padding", "border", "position", "top", "left",
		"right", "bottom", "overflow", "opacity", "zIndex", "textAlign", "fontWeight"}
	for _, prop := range styleProps {
		p := prop // capture
		styleObj.DefineGetter(p, func(args []*espresso.Value) *espresso.Value {
			if val, ok := el.StyleMap[p]; ok {
				return espresso.NewStr(val)
			}
			return espresso.NewStr("")
		})
		styleObj.DefineSetter(p, func(args []*espresso.Value) *espresso.Value {
			if len(args) > 0 {
				el.StyleMap[p] = args[0].String()
			}
			return espresso.Undefined
		})
	}
	v.Object()["style"] = styleObj

	// classList object
	classList := espresso.NewObj(map[string]*espresso.Value{})
	classList.Object()["add"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		for _, a := range args {
			cls := a.String()
			found := false
			for _, c := range el.ClassList {
				if c == cls { found = true; break }
			}
			if !found {
				el.ClassList = append(el.ClassList, cls)
				el.Attrs["class"] = strings.Join(el.ClassList, " ")
			}
		}
		return espresso.Undefined
	})
	classList.Object()["remove"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		for _, a := range args {
			cls := a.String()
			for i, c := range el.ClassList {
				if c == cls {
					el.ClassList = append(el.ClassList[:i], el.ClassList[i+1:]...)
					break
				}
			}
		}
		el.Attrs["class"] = strings.Join(el.ClassList, " ")
		return espresso.Undefined
	})
	classList.Object()["contains"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewBool(false) }
		cls := args[0].String()
		for _, c := range el.ClassList {
			if c == cls { return espresso.NewBool(true) }
		}
		return espresso.NewBool(false)
	})
	classList.Object()["toggle"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewBool(false) }
		cls := args[0].String()
		for i, c := range el.ClassList {
			if c == cls {
				el.ClassList = append(el.ClassList[:i], el.ClassList[i+1:]...)
				el.Attrs["class"] = strings.Join(el.ClassList, " ")
				return espresso.NewBool(false)
			}
		}
		el.ClassList = append(el.ClassList, cls)
		el.Attrs["class"] = strings.Join(el.ClassList, " ")
		return espresso.NewBool(true)
	})
	v.Object()["classList"] = classList

	el.jsValue = v
	return v
}

func textToJS(tn *TextNode) *espresso.Value {
	if tn.jsValue != nil {
		return tn.jsValue
	}
	v := espresso.NewObj(map[string]*espresso.Value{
		"nodeType": espresso.NewNum(NodeText),
		"nodeName": espresso.NewStr("#text"),
	})
	v.DefineGetter("textContent", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(tn.Data)
	})
	v.DefineSetter("textContent", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 { tn.Data = args[0].String() }
		return espresso.Undefined
	})
	v.DefineGetter("data", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(tn.Data)
	})
	v.DefineGetter("nodeValue", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(tn.Data)
	})
	v.DefineGetter("parentNode", func(args []*espresso.Value) *espresso.Value {
		return NodeToJS(tn.Parent)
	})
	tn.jsValue = v
	return v
}

// DocumentToJS exposes the document to espresso.
func DocumentToJS(doc *Document) *espresso.Value {
	if doc.jsValue != nil {
		return doc.jsValue
	}
	v := espresso.NewObj(map[string]*espresso.Value{
		"nodeType": espresso.NewNum(NodeDocument),
		"nodeName": espresso.NewStr("#document"),
	})

	v.DefineGetter("title", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewStr(doc.Title)
	})
	v.DefineSetter("title", func(args []*espresso.Value) *espresso.Value {
		if len(args) > 0 { doc.Title = args[0].String() }
		return espresso.Undefined
	})

	// documentElement (html), head, body — computed from children
	v.DefineGetter("documentElement", func(args []*espresso.Value) *espresso.Value {
		for _, c := range doc.Children {
			if el := nodeToElement(c); el != nil && el.TagName == "HTML" {
				return NodeToJS(c)
			}
		}
		return espresso.Null
	})
	v.DefineGetter("head", func(args []*espresso.Value) *espresso.Value {
		return findChildElementJS(doc, "HEAD")
	})
	v.DefineGetter("body", func(args []*espresso.Value) *espresso.Value {
		return findChildElementJS(doc, "BODY")
	})
	v.DefineGetter("childNodes", func(args []*espresso.Value) *espresso.Value {
		arr := make([]*espresso.Value, len(doc.Children))
		for i, c := range doc.Children {
			arr[i] = NodeToJS(c)
		}
		return espresso.NewArr(arr)
	})

	// createElement
	v.Object()["createElement"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		el := NewElement(args[0].String())
		el.OwnerDoc = doc
		RegisterNode(&el.Node, el)
		return elementToJS(el)
	})

	// createTextNode
	v.Object()["createTextNode"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		data := ""
		if len(args) > 0 { data = args[0].String() }
		tn := NewTextNode(data)
		tn.OwnerDoc = doc
		RegisterNode(&tn.Node, tn)
		return textToJS(tn)
	})

	// getElementById
	v.Object()["getElementById"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.Null }
		id := args[0].String()
		el := findByID(&doc.Node, id)
		if el != nil {
			return elementToJS(el)
		}
		return espresso.Null
	})

	// getElementsByTagName
	v.Object()["getElementsByTagName"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewArr(nil) }
		tag := strings.ToUpper(args[0].String())
		var results []*espresso.Value
		findByTag(&doc.Node, tag, &results)
		return espresso.NewArr(results)
	})

	// getElementsByClassName
	v.Object()["getElementsByClassName"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 { return espresso.NewArr(nil) }
		cls := args[0].String()
		var results []*espresso.Value
		findByClass(&doc.Node, cls, &results)
		return espresso.NewArr(results)
	})

	// addEventListener / removeEventListener / dispatchEvent on document
	docListeners := map[string][]*espresso.Value{}
	v.Object()["addEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		docListeners[event] = append(docListeners[event], cb)
		return espresso.Undefined
	})
	v.Object()["removeEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		listeners := docListeners[event]
		for i, l := range listeners {
			if l == cb {
				docListeners[event] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		return espresso.Undefined
	})
	v.Object()["dispatchEvent"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 1 {
			return espresso.NewBool(false)
		}
		eventObj := args[0]
		eventType := ""
		if eventObj.IsObject() {
			t := eventObj.Get("type")
			if t != nil && !t.IsUndefined() {
				eventType = t.String()
			}
		} else {
			eventType = eventObj.String()
		}
		for i, cb := range docListeners[eventType] {
			dispatchCallback(cb, eventObj, i)
		}
		return espresso.NewBool(true)
	})

	doc.jsValue = v
	return v
}

// ─── DOM search helpers ─────────────────────────────────────

func findByID(n *Node, id string) *Element {
	if el := nodeToElement(n); el != nil && el.ID == id {
		return el
	}
	for _, c := range n.Children {
		if found := findByID(c, id); found != nil {
			return found
		}
	}
	return nil
}

func findByTag(n *Node, tag string, results *[]*espresso.Value) {
	if el := nodeToElement(n); el != nil && el.TagName == tag {
		*results = append(*results, elementToJS(el))
	}
	for _, c := range n.Children {
		findByTag(c, tag, results)
	}
}

func findByClass(n *Node, cls string, results *[]*espresso.Value) {
	if el := nodeToElement(n); el != nil {
		for _, c := range el.ClassList {
			if c == cls {
				*results = append(*results, elementToJS(el))
				break
			}
		}
	}
	for _, c := range n.Children {
		findByClass(c, cls, results)
	}
}

func findChildElementJS(doc *Document, tag string) *espresso.Value {
	html := findChildElement(&doc.Node, "HTML")
	if html == nil {
		return espresso.Null
	}
	el := findChildElement(&html.Node, tag)
	if el != nil {
		return elementToJS(el)
	}
	return espresso.Null
}

func findChildElement(n *Node, tag string) *Element {
	for _, c := range n.Children {
		if el := nodeToElement(c); el != nil && el.TagName == tag {
			return el
		}
		if found := findChildElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

func CollectTextFromElement(el *Element, sb *strings.Builder) {
	for _, child := range el.Children {
		if tn := nodeToText(child); tn != nil {
			sb.WriteString(tn.Data)
		} else if cel := nodeToElement(child); cel != nil {
			CollectTextFromElement(cel, sb)
		}
	}
}
