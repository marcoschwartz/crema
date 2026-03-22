package crema

import (
	"os"
	"strings"
	"testing"
)

// ══════════════════════════════════════════════════════════════
// DOM Tests
// ══════════════════════════════════════════════════════════════

func TestDOM_CreateElement(t *testing.T) {
	el := NewElement("div")
	RegisterNode(&el.Node, el)
	if el.TagName != "DIV" { t.Errorf("expected DIV, got %s", el.TagName) }
	if el.NodeType != NodeElement { t.Errorf("expected %d, got %d", NodeElement, el.NodeType) }
}

func TestDOM_Attributes(t *testing.T) {
	el := NewElement("a")
	RegisterNode(&el.Node, el)
	el.SetAttribute("href", "https://example.com")
	el.SetAttribute("class", "link active")
	el.SetAttribute("id", "main-link")

	if el.GetAttribute("href") != "https://example.com" { t.Error("href") }
	if el.ID != "main-link" { t.Error("id") }
	if len(el.ClassList) != 2 { t.Errorf("expected 2 classes, got %d", len(el.ClassList)) }
	if !el.HasAttribute("href") { t.Error("should have href") }

	el.RemoveAttribute("href")
	if el.HasAttribute("href") { t.Error("should not have href") }
}

func TestDOM_TreeOperations(t *testing.T) {
	parent := NewElement("div")
	RegisterNode(&parent.Node, parent)
	child1 := NewElement("span")
	RegisterNode(&child1.Node, child1)
	child2 := NewElement("p")
	RegisterNode(&child2.Node, child2)

	parent.AppendChild(&child1.Node)
	parent.AppendChild(&child2.Node)

	if len(parent.Children) != 2 { t.Errorf("expected 2 children, got %d", len(parent.Children)) }
	if parent.FirstChild() != &child1.Node { t.Error("firstChild") }
	if parent.LastChild() != &child2.Node { t.Error("lastChild") }
	if child1.NextSibling() != &child2.Node { t.Error("nextSibling") }
	if child2.PreviousSibling() != &child1.Node { t.Error("previousSibling") }

	parent.RemoveChild(&child1.Node)
	if len(parent.Children) != 1 { t.Error("after remove") }
}

func TestDOM_TextContent(t *testing.T) {
	el := NewElement("p")
	RegisterNode(&el.Node, el)
	tn := NewTextNode("Hello World")
	RegisterNode(&tn.Node, tn)
	el.AppendChild(&tn.Node)

	var sb strings.Builder
	CollectTextFromElement(el, &sb)
	if sb.String() != "Hello World" { t.Errorf("expected 'Hello World', got '%s'", sb.String()) }
}

func TestDOM_InnerHTML(t *testing.T) {
	parent := NewElement("div")
	RegisterNode(&parent.Node, parent)
	child := NewElement("span")
	RegisterNode(&child.Node, child)
	tn := NewTextNode("hello")
	RegisterNode(&tn.Node, tn)
	child.AppendChild(&tn.Node)
	parent.AppendChild(&child.Node)

	html := parent.InnerHTML()
	if html != "<span>hello</span>" { t.Errorf("expected '<span>hello</span>', got '%s'", html) }
}

// ══════════════════════════════════════════════════════════════
// HTML Parser Tests
// ══════════════════════════════════════════════════════════════

func TestParser_BasicHTML(t *testing.T) {
	doc := ParseHTML(`<html><head><title>Test</title></head><body><h1>Hello</h1><p>World</p></body></html>`)
	if doc.Title != "Test" { t.Errorf("expected title 'Test', got '%s'", doc.Title) }

	body := findChildElement(&doc.Node, "BODY")
	if body == nil { t.Fatal("no body found") }
	if len(body.Children) < 2 { t.Fatalf("expected at least 2 children in body, got %d", len(body.Children)) }

	h1 := nodeToElement(body.Children[0])
	if h1 == nil || h1.TagName != "H1" { t.Error("first child should be H1") }
}

func TestParser_Attributes(t *testing.T) {
	doc := ParseHTML(`<div id="main" class="container active" data-value="42"></div>`)
	el := QuerySelector(&doc.Node, "#main")
	if el == nil { t.Fatal("should find #main") }
	if el.ID != "main" { t.Error("id") }
	if len(el.ClassList) != 2 { t.Errorf("expected 2 classes, got %d", len(el.ClassList)) }
	if el.GetAttribute("data-value") != "42" { t.Error("data-value") }
}

func TestParser_Scripts(t *testing.T) {
	doc := ParseHTML(`<html><body><script>let x = 1;</script><script src="app.js"></script></body></html>`)
	scripts := ExtractScripts(doc)
	if len(scripts) != 2 { t.Fatalf("expected 2 scripts, got %d", len(scripts)) }
	if !scripts[0].Inline { t.Error("first should be inline") }
	if scripts[0].Code != "let x = 1;" { t.Errorf("code: '%s'", scripts[0].Code) }
	if scripts[1].Inline { t.Error("second should be external") }
	if scripts[1].Src != "app.js" { t.Error("src") }
}

// ══════════════════════════════════════════════════════════════
// Selector Tests
// ══════════════════════════════════════════════════════════════

func TestSelector_Tag(t *testing.T) {
	doc := ParseHTML(`<div><p>one</p><p>two</p><span>three</span></div>`)
	results := QuerySelectorAll(&doc.Node, "p")
	if len(results) != 2 { t.Errorf("expected 2 <p>, got %d", len(results)) }
}

func TestSelector_Class(t *testing.T) {
	doc := ParseHTML(`<div><p class="active">one</p><p>two</p><p class="active">three</p></div>`)
	results := QuerySelectorAll(&doc.Node, ".active")
	if len(results) != 2 { t.Errorf("expected 2 .active, got %d", len(results)) }
}

func TestSelector_ID(t *testing.T) {
	doc := ParseHTML(`<div><p id="target">found</p><p>other</p></div>`)
	el := QuerySelector(&doc.Node, "#target")
	if el == nil { t.Fatal("should find #target") }
	if el.TagName != "P" { t.Error("should be <p>") }
}

func TestSelector_TagAndClass(t *testing.T) {
	doc := ParseHTML(`<div><p class="x">yes</p><span class="x">no</span></div>`)
	results := QuerySelectorAll(&doc.Node, "p.x")
	if len(results) != 1 { t.Errorf("expected 1 p.x, got %d", len(results)) }
}

func TestSelector_Attribute(t *testing.T) {
	doc := ParseHTML(`<div><input type="text"><input type="password"><input type="text"></div>`)
	results := QuerySelectorAll(&doc.Node, `input[type="text"]`)
	if len(results) != 2 { t.Errorf("expected 2 input[type=text], got %d", len(results)) }
}

func TestSelector_Descendant(t *testing.T) {
	doc := ParseHTML(`<div class="outer"><div class="inner"><p>deep</p></div></div><p>shallow</p>`)
	results := QuerySelectorAll(&doc.Node, ".outer p")
	if len(results) != 1 { t.Errorf("expected 1 .outer p, got %d", len(results)) }
}

func TestSelector_Multiple(t *testing.T) {
	doc := ParseHTML(`<div><h1>title</h1><h2>subtitle</h2><p>text</p></div>`)
	results := QuerySelectorAll(&doc.Node, "h1, h2")
	if len(results) != 2 { t.Errorf("expected 2 (h1+h2), got %d", len(results)) }
}

// ══════════════════════════════════════════════════════════════
// JS + DOM Integration Tests
// ══════════════════════════════════════════════════════════════

func TestJS_DocumentQuerySelector(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="app"><p class="greeting">Hello</p></div></body></html>`)

	r, _ := p.Eval(`document.querySelector("#app").tagName`)
	if r.String() != "DIV" { t.Errorf("expected DIV, got '%s'", r.String()) }

	r2, _ := p.Eval(`document.querySelector(".greeting").textContent`)
	if r2.String() != "Hello" { t.Errorf("expected 'Hello', got '%s'", r2.String()) }
}

func TestJS_DocumentGetElementById(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="main">content</div></body></html>`)

	r, _ := p.Eval(`document.getElementById("main").textContent`)
	if r.String() != "content" { t.Errorf("expected 'content', got '%s'", r.String()) }
}

func TestJS_ElementAttributes(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><a id="link" href="https://example.com" class="btn primary">Click</a></body></html>`)

	r, _ := p.Eval(`document.getElementById("link").getAttribute("href")`)
	if r.String() != "https://example.com" { t.Errorf("href: %s", r.String()) }

	r2, _ := p.Eval(`document.getElementById("link").className`)
	if r2.String() != "btn primary" { t.Errorf("className: %s", r2.String()) }

	r3, _ := p.Eval(`document.getElementById("link").classList.contains("btn")`)
	if !r3.Bool() { t.Error("should contain btn") }
}

func TestJS_InlineScript(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="target">original</div><script>
		const el = document.getElementById("target");
		el.textContent = "modified";
	</script></body></html>`)

	el := p.QuerySelector("#target")
	if el == nil { t.Fatal("should find #target") }
	var sb strings.Builder
	CollectTextFromElement(el, &sb)
	if sb.String() != "modified" { t.Errorf("expected 'modified', got '%s'", sb.String()) }
}

func TestJS_DocumentTitle(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><head><title>My Page</title></head><body></body></html>`)

	r, _ := p.Eval(`document.title`)
	if r.String() != "My Page" { t.Errorf("expected 'My Page', got '%s'", r.String()) }
}

func TestJS_Navigator(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body></body></html>`)

	r, _ := p.Eval(`navigator.webdriver`)
	if r.Bool() { t.Error("webdriver should be false") }

	r2, _ := p.Eval(`navigator.language`)
	if r2.String() != "en-US" { t.Errorf("expected en-US, got %s", r2.String()) }
}

func TestJS_ClassListToggle(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="box" class="visible"></div></body></html>`)

	p.Eval(`document.getElementById("box").classList.toggle("visible")`)
	el := p.QuerySelector("#box")
	if len(el.ClassList) != 0 { t.Error("should have removed visible") }

	p.Eval(`document.getElementById("box").classList.toggle("active")`)
	if len(el.ClassList) != 1 || el.ClassList[0] != "active" { t.Error("should have added active") }
}

// ══════════════════════════════════════════════════════════════
// Screenshot Tests
// ══════════════════════════════════════════════════════════════

func TestScreenshot_BasicHTML(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body>
		<h1>Hello Crema</h1>
		<p>This is a test page with some content.</p>
		<a href="https://example.com">A link</a>
		<div style="background-color: #eee; padding: 10px;">
			<strong>Bold text</strong> and <em>italic text</em>
		</div>
		<ul>
			<li>Item one</li>
			<li>Item two</li>
			<li>Item three</li>
		</ul>
		<input type="text" placeholder="Enter your name">
		<button>Click me</button>
	</body></html>`)

	err := p.ScreenshotFile("/tmp/crema_test.png")
	if err != nil { t.Fatalf("screenshot error: %v", err) }
	t.Log("Screenshot saved to /tmp/crema_test.png")

	// Verify file exists and has content
	info, err := os.Stat("/tmp/crema_test.png")
	if err != nil { t.Fatalf("file not found: %v", err) }
	if info.Size() < 100 { t.Error("screenshot file too small") }
	t.Logf("Screenshot size: %d bytes", info.Size())
}

func TestScreenshot_RealPage(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()

	err := p.Navigate("https://example.com")
	if err != nil { t.Fatalf("navigate: %v", err) }

	err = p.ScreenshotFile("/tmp/crema_example.png")
	if err != nil { t.Fatalf("screenshot: %v", err) }
	t.Log("Screenshot of example.com saved to /tmp/crema_example.png")

	info, _ := os.Stat("/tmp/crema_example.png")
	t.Logf("Size: %d bytes", info.Size())
}

// ══════════════════════════════════════════════════════════════
// Real Webpage Test
// ══════════════════════════════════════════════════════════════

func TestBrowser_FetchRealPage(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()

	err := p.Navigate("https://example.com")
	if err != nil { t.Fatalf("navigate: %v", err) }

	// Check title
	title := p.Title()
	if title == "" { t.Error("page should have a title") }
	t.Logf("Title: %s", title)

	// Check DOM
	h1 := p.QuerySelector("h1")
	if h1 == nil { t.Fatal("should find <h1>") }
	var sb strings.Builder
	CollectTextFromElement(h1, &sb)
	t.Logf("H1: %s", sb.String())
	if sb.String() == "" { t.Error("h1 should have text") }

	// Check links
	links := p.QuerySelectorAll("a")
	t.Logf("Links found: %d", len(links))
	for _, link := range links {
		t.Logf("  <a href=\"%s\">%s</a>", link.GetAttribute("href"), link.GetAttribute("href"))
	}

	// JS access to DOM
	r, _ := p.Eval(`document.title`)
	if r.String() != title { t.Errorf("JS title mismatch: %s vs %s", r.String(), title) }

	r2, _ := p.Eval(`document.querySelector("h1").textContent`)
	if r2.String() == "" { t.Error("JS h1 textContent empty") }
	t.Logf("JS h1.textContent: %s", r2.String())
}

// ══════════════════════════════════════════════════════════════
// Event System Tests
// ══════════════════════════════════════════════════════════════

func TestJS_AddEventListener(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="btn">click me</div><div id="result">none</div></body></html>`)

	// Test element addEventListener — use DOM mutation to verify callback runs
	p.VM.Run(`
		var el = document.getElementById("btn");
		var result = document.getElementById("result");
		el.addEventListener("click", (ev) => {
			el.setAttribute("data-clicked", "true");
			result.textContent = "clicked";
		});
		el.dispatchEvent({type: "click"});
	`)

	el := p.QuerySelector("#btn")
	if el.GetAttribute("data-clicked") != "true" {
		t.Error("expected data-clicked='true' after element dispatchEvent")
	}
	resultEl := p.QuerySelector("#result")
	var sb strings.Builder
	CollectTextFromElement(resultEl, &sb)
	if sb.String() != "clicked" {
		t.Errorf("expected result text 'clicked', got '%s'", sb.String())
	}

	// Test document addEventListener
	p.VM.Run(`
		document.addEventListener("DOMContentLoaded", (ev) => {
			document.getElementById("btn").setAttribute("data-doc", "loaded");
		});
		document.dispatchEvent({type: "DOMContentLoaded"});
	`)
	if el.GetAttribute("data-doc") != "loaded" {
		t.Error("expected data-doc='loaded' after document dispatchEvent")
	}

	// Test window addEventListener
	p.VM.Run(`
		window.addEventListener("load", (ev) => {
			document.getElementById("btn").setAttribute("data-win", "loaded");
		});
		window.dispatchEvent({type: "load"});
	`)
	if el.GetAttribute("data-win") != "loaded" {
		t.Error("expected data-win='loaded' after window dispatchEvent")
	}
}

func TestJS_ElementStyle(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="box">hello</div></body></html>`)

	// Set style properties via a single Run block
	p.VM.Run(`
		var box = document.getElementById("box");
		box.style.display = "none";
		box.style.color = "red";
		box.style.backgroundColor = "blue";
	`)

	// Read them back via Go-side check
	el := p.QuerySelector("#box")
	if el.StyleMap["display"] != "none" {
		t.Errorf("expected display 'none', got '%s'", el.StyleMap["display"])
	}
	if el.StyleMap["color"] != "red" {
		t.Errorf("expected color 'red', got '%s'", el.StyleMap["color"])
	}
	if el.StyleMap["backgroundColor"] != "blue" {
		t.Errorf("expected backgroundColor 'blue', got '%s'", el.StyleMap["backgroundColor"])
	}

	// Read via JS
	r1, _ := p.Eval(`document.getElementById("box").style.display`)
	if r1.String() != "none" {
		t.Errorf("expected display 'none' via JS, got '%s'", r1.String())
	}

	// Unset property should return empty string
	r4, _ := p.Eval(`document.getElementById("box").style.fontSize`)
	if r4.String() != "" {
		t.Errorf("expected empty fontSize, got '%s'", r4.String())
	}
}

func TestJS_DocumentCookie(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body></body></html>`)

	// Set cookies via VM.Run (statements need Run, not Eval)
	p.VM.Run(`document.cookie = "name=alice";`)
	p.VM.Run(`document.cookie = "session=abc123";`)

	// Read cookie string via JS
	r, _ := p.Eval(`document.cookie`)
	cookieStr := r.String()
	if !strings.Contains(cookieStr, "name=alice") {
		t.Errorf("cookie string should contain 'name=alice', got '%s'", cookieStr)
	}
	if !strings.Contains(cookieStr, "session=abc123") {
		t.Errorf("cookie string should contain 'session=abc123', got '%s'", cookieStr)
	}

	// Verify the Go-side Cookies map
	if p.Cookies["name"] != "alice" {
		t.Errorf("expected Cookies[name] = alice, got %s", p.Cookies["name"])
	}
	if p.Cookies["session"] != "abc123" {
		t.Errorf("expected Cookies[session] = abc123, got %s", p.Cookies["session"])
	}

	// Overwrite a cookie
	p.VM.Run(`document.cookie = "name=bob";`)
	if p.Cookies["name"] != "bob" {
		t.Errorf("expected Cookies[name] = bob after overwrite, got %s", p.Cookies["name"])
	}
}

// ── Lifecycle events ──

func TestJS_DOMContentLoaded(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body>
		<div id="status">waiting</div>
		<script>
			document.addEventListener("DOMContentLoaded", function() {
				document.getElementById("status").textContent = "loaded";
			});
		</script>
	</body></html>`)

	r, _ := p.Eval(`document.getElementById("status").textContent`)
	if r.String() != "loaded" { t.Errorf("expected 'loaded', got '%s'", r.String()) }
}

func TestJS_WindowLoad(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body>
		<div id="status">waiting</div>
		<script>
			window.addEventListener("load", function() {
				document.getElementById("status").textContent = "window loaded";
			});
		</script>
	</body></html>`)

	r, _ := p.Eval(`document.getElementById("status").textContent`)
	if r.String() != "window loaded" { t.Errorf("expected 'window loaded', got '%s'", r.String()) }
}

func TestJS_DOMContentLoaded_Arrow(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body>
		<div id="s">0</div>
		<script>
			document.addEventListener("DOMContentLoaded", () => {
				document.getElementById("s").textContent = "1";
			});
		</script>
	</body></html>`)

	r, _ := p.Eval(`document.getElementById("s").textContent`)
	if r.String() != "1" { t.Errorf("expected '1', got '%s'", r.String()) }
}

// ── Lifecycle events ──

func TestJS_DOMContentLoaded_Direct(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body>
		<div id="status">waiting</div>
		<script>
			document.addEventListener("DOMContentLoaded", function() {
				document.getElementById("status").textContent = "loaded";
			});
		</script>
	</body></html>`)

	// Check DOM directly via Go
	el := p.QuerySelector("#status")
	if el != nil {
		t.Logf("DOM element found, children: %d", len(el.Children))
		for _, c := range el.Children {
			if tn := nodeToText(c); tn != nil {
				t.Logf("  text: %q", tn.Data)
			}
		}
	}

	// Also check via Eval
	r, _ := p.Eval(`document.getElementById("status").textContent`)
	t.Logf("JS textContent: %q", r.String())
	t.Logf("docListeners DOMContentLoaded: %d", len(p.docListeners["DOMContentLoaded"]))
}

func TestJS_ManualCallback(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="s">before</div></body></html>`)

	// Manually call a function that modifies the DOM
	p.VM.Run(`function myFn() { document.getElementById("s").textContent = "after"; }`)
	p.VM.Run(`myFn();`)

	r, _ := p.Eval(`document.getElementById("s").textContent`)
	t.Logf("after myFn: %q", r.String())

	// Check Go DOM
	el := p.QuerySelector("#s")
	if el != nil {
		for _, c := range el.Children {
			if tn := nodeToText(c); tn != nil {
				t.Logf("Go DOM text: %q", tn.Data)
			}
		}
	}
}

func TestJS_DirectSetter(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="s">before</div></body></html>`)

	// Direct one-liner
	p.VM.Run(`document.getElementById("s").textContent = "after";`)

	el := p.QuerySelector("#s")
	if el != nil {
		for _, c := range el.Children {
			if tn := nodeToText(c); tn != nil {
				t.Logf("text: %q", tn.Data)
			}
		}
	}
	r, _ := p.Eval(`document.getElementById("s").textContent`)
	t.Logf("JS: %q", r.String())
}

func TestJS_ElementMatches(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div id="x" class="foo bar"></div></body></html>`)

	r, _ := p.Eval(`document.getElementById("x").matches(".foo")`)
	if !r.Bool() { t.Error("should match .foo") }
	r2, _ := p.Eval(`document.getElementById("x").matches(".baz")`)
	if r2.Bool() { t.Error("should not match .baz") }
	r3, _ := p.Eval(`document.getElementById("x").matches("div.foo")`)
	if !r3.Bool() { t.Error("should match div.foo") }
}

func TestJS_ElementClosest(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><body><div class="outer"><div class="inner"><span id="s">hi</span></div></div></body></html>`)

	r, _ := p.Eval(`document.getElementById("s").closest(".inner").className`)
	if r.String() != "inner" { t.Errorf("expected 'inner', got '%s'", r.String()) }
	r2, _ := p.Eval(`document.getElementById("s").closest(".outer").className`)
	if r2.String() != "outer" { t.Errorf("expected 'outer', got '%s'", r2.String()) }
	r3, _ := p.Eval(`document.getElementById("s").closest(".nope")`)
	if !r3.IsNull() { t.Error("should be null for no match") }
}

func TestCSS_StyleTagHidesElement(t *testing.T) {
	b := NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.LoadHTML(`<html><head><style>.hidden { display: none; } .invisible { visibility: hidden; }</style></head><body>
		<div class="hidden">should not render</div>
		<div id="visible">should render</div>
	</body></html>`)

	// The hidden div should not appear in layout
	root := Layout(p.Doc, 800, 400)
	found := false
	checkBoxes(root, "should not render", &found)
	if found { t.Error("hidden element should not be in layout") }

	// Visible div should be there
	el := p.QuerySelector("#visible")
	if el == nil { t.Error("visible div should exist") }
}

func checkBoxes(b *Box, text string, found *bool) {
	if b.Text == text { *found = true }
	for _, c := range b.Children { checkBoxes(c, text, found) }
}
