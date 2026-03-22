package crema

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcoschwartz/espresso"
)

// Browser is a headless browser instance.
type Browser struct {
	UserAgent string
	Proxy     string // proxy URL (http, https, socks5)
	Pages     []*Page
	Client    *http.Client
}

// Page represents a single browser tab/page.
type Page struct {
	URL        string
	Doc        *Document
	VM         *espresso.VM
	Browser    *Browser
	UserAgent  string
	Scripts    []Script
	LastLayout *Box // cached layout for hit testing
	Cookies    map[string]string
	docListeners map[string][]*espresso.Value // document event listeners
	winListeners map[string][]*espresso.Value // window event listeners
}

// NewBrowser creates a new headless browser.
func NewBrowser() *Browser {
	return &Browser{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: ChromeTransport(),
		},
	}
}

// NewBrowserWithProxy creates a browser that routes all traffic through a proxy.
func NewBrowserWithProxy(proxyURL string) *Browser {
	return &Browser{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Proxy:     proxyURL,
		Client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: ProxyTransport(proxyURL),
		},
	}
}

// NewPage creates a new blank page.
func (b *Browser) NewPage() *Page {
	p := &Page{
		Browser:   b,
		UserAgent: b.UserAgent,
		VM:           espresso.New(),
		Cookies:      make(map[string]string),
		docListeners: make(map[string][]*espresso.Value),
		winListeners: make(map[string][]*espresso.Value),
	}
	b.Pages = append(b.Pages, p)
	return p
}

// Navigate fetches a URL, parses the HTML, sets up the DOM, and executes scripts.
func (p *Page) Navigate(url string) error {
	p.URL = url

	// Fetch with hard timeout
	type result struct {
		body []byte
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		b, e := p.doFetch(url)
		ch <- result{b, e}
	}()

	var body []byte
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		body = r.body
	case <-time.After(15 * time.Second):
		return fmt.Errorf("fetch timeout after 15s")
	}

	// Parse HTML → DOM
	p.Doc = ParseHTML(string(body))

	// Set up JS environment
	p.setupJS()

	// Execute scripts with timeout
	p.Scripts = ExtractScripts(p.Doc)
	p.executeScriptsWithTimeout(5 * time.Second)

	// Fire lifecycle events
	p.fireLifecycleEvents()

	// Simulate lazy-load: copy data-src → src for images that haven't been loaded
	simulateLazyLoad(p.Doc)

	return nil
}

func (p *Page) doFetch(url string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", p.UserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	// Add cookies to request
	for name, val := range p.Cookies {
		req.AddCookie(&http.Cookie{Name: name, Value: val})
	}

	resp, err := p.Browser.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	// Collect Set-Cookie headers
	for _, cookie := range resp.Cookies() {
		p.Cookies[cookie.Name] = cookie.Value
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("reading body: %w", err)
	}
	return body, nil
}

// LoadHTML loads HTML from a string (no network request).
func (p *Page) LoadHTML(html string) {
	p.URL = "about:blank"
	p.Doc = ParseHTML(html)
	p.setupJS()
	p.Scripts = ExtractScripts(p.Doc)
	p.executeScriptsWithTimeout(5 * time.Second)
	p.fireLifecycleEvents()
}

// setupJS initializes the espresso VM with document, window, and Web APIs.
func (p *Page) setupJS() {
	activeVM = p.VM
	docJS := DocumentToJS(p.Doc)
	AddQuerySelectors(docJS, p.Doc)

	// document.cookie getter/setter
	docJS.DefineGetter("cookie", func(args []*espresso.Value) *espresso.Value {
		var parts []string
		for k, v := range p.Cookies {
			parts = append(parts, k+"="+v)
		}
		return espresso.NewStr(strings.Join(parts, "; "))
	})
	docJS.DefineSetter("cookie", func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			return espresso.Undefined
		}
		raw := args[0].String()
		// Parse "name=value; path=/; ..." — only take the name=value part
		parts := strings.SplitN(raw, ";", 2)
		kv := strings.SplitN(strings.TrimSpace(parts[0]), "=", 2)
		if len(kv) == 2 {
			p.Cookies[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
		}
		return espresso.Undefined
	})

	// document.addEventListener / removeEventListener
	docJS.Object()["addEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) >= 2 {
			event := args[0].String()
			p.docListeners[event] = append(p.docListeners[event], args[1])
		}
		return espresso.Undefined
	})
	docJS.Object()["dispatchEvent"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 1 { return espresso.NewBool(false) }
		eventType := ""
		if args[0].IsObject() {
			t := args[0].Get("type")
			if t != nil && !t.IsUndefined() { eventType = t.String() }
		} else {
			eventType = args[0].String()
		}
		for _, cb := range p.docListeners[eventType] {
			espresso.CallFuncValue(cb, []*espresso.Value{args[0]}, p.VM.Scope())
		}
		return espresso.NewBool(true)
	})
	docJS.Object()["removeEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) >= 2 {
			event := args[0].String()
			cb := args[1]
			listeners := p.docListeners[event]
			for i, l := range listeners {
				if l == cb { p.docListeners[event] = append(listeners[:i], listeners[i+1:]...); break }
			}
		}
		return espresso.Undefined
	})

	p.VM.SetValue("document", docJS)
	RegisterWebAPIs(p.VM, p)

	// Inject jQuery shim as native Go functions
	injectjQuery(p.VM, p)
}

// Non-essential script domains — these are ads, analytics, tracking, and CDN
// scripts that don't affect page content. Skipping them dramatically improves
// page load speed without losing any visible content.
var skipScriptDomains = []string{
	"googletagmanager.com", "google-analytics.com", "googleads",
	"googlesyndication.com", "googleadservices.com",
	"facebook.net", "fbevents.js", "connect.facebook",
	"cloudflareinsights.com", "cloudflare-static",
	"cdn-cgi/scripts", "challenge-platform",
	"adsbygoogle", "adpushup", "adsense",
	"doubleclick.net", "amazon-adsystem.com",
	"hotjar.com", "clarity.ms", "segment.com",
	"optimizely.com", "mixpanel.com", "amplitude.com",
	"newrelic.com", "nr-data.net",
	"sentry.io", "bugsnag.com",
	"recaptcha", "hcaptcha.com", "turnstile",
	"twitter.com/widgets", "platform.twitter",
	"linkedin.com/insight", "snap.licdn.com",
	"pinterest.com", "tiktok.com",
	"disqus.com", "livechat", "intercom",
	"beacon.min.js", "rocket-loader",
	// Captchas (can't solve them, they block forever)
	"turnstile", "challenges.cloudflare",
	"recaptcha", "hcaptcha.com",
}

// Scripts we skip downloading but still provide shims for.
// jQuery is shimmed natively. Others get empty stubs so dependent code doesn't crash.
var shimmedScriptDomains = []string{
	"jquery.min.js", "jquery-ui",
}

func shouldSkipScript(src string) bool {
	lower := strings.ToLower(src)
	for _, domain := range skipScriptDomains {
		if strings.Contains(lower, domain) {
			return true
		}
	}
	// Shimmed scripts — skip download but shim is already injected
	for _, domain := range shimmedScriptDomains {
		if strings.Contains(lower, domain) {
			return true
		}
	}
	return false
}

// executeScriptsWithTimeout runs inline and essential external scripts.
func (p *Page) executeScriptsWithTimeout(timeout time.Duration) {
	for _, script := range p.Scripts {
		code := ""
		if script.Inline {
			code = script.Code
		} else if script.Src != "" {
			// Skip non-essential scripts (ads, analytics, tracking)
			if shouldSkipScript(script.Src) {
				continue
			}
			// Resolve relative URLs
			srcURL := script.Src
			if strings.HasPrefix(srcURL, "//") {
				srcURL = "https:" + srcURL
			} else if strings.HasPrefix(srcURL, "/") && p.URL != "" && p.URL != "about:blank" {
				srcURL = extractOrigin(p.URL) + srcURL
			}
			if strings.HasPrefix(srcURL, "http") {
				fetched, err := p.fetchExternalScript(srcURL)
				if err != nil {
					continue
				}
				code = fetched
			} else {
				continue
			}
		} else {
			continue
		}

		done := make(chan bool, 1)
		go func(c string) {
			defer func() { recover() }()
			p.VM.Run(c)
			done <- true
		}(code)

		scriptTimeout := timeout
		if !script.Inline {
			scriptTimeout = 500 * time.Millisecond
		}
		select {
		case <-done:
		case <-time.After(scriptTimeout):
		}
	}
}

// fetchExternalScript fetches the content of an external script URL.
func (p *Page) fetchExternalScript(url string) (string, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", p.UserAgent)
	resp, err := p.Browser.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// fireLifecycleEvents dispatches DOMContentLoaded on document and load on window.
func (p *Page) fireLifecycleEvents() {
	fireAll := func(listeners []*espresso.Value) {
		for _, cb := range listeners {
			if cb.Type() == espresso.TypeFunc {
				p.VM.SetValue("__lcb__", cb)
				// Use CompileAndRun which uses the VM scope directly
				p.VM.CompileAndRun("__lcb__();")
			}
		}
	}
	fireAll(p.docListeners["DOMContentLoaded"])
	fireAll(p.winListeners["load"])
	fireAll(p.docListeners["readystatechange"])
}

// SubmitForm submits a form element via HTTP and navigates to the result.
func (p *Page) SubmitForm(form *Element) error {
	action := form.GetAttribute("action")
	if action == "" { action = p.URL }
	method := strings.ToUpper(form.GetAttribute("method"))
	if method == "" { method = "GET" }

	// Resolve relative action
	if strings.HasPrefix(action, "/") && p.URL != "" {
		action = extractOrigin(p.URL) + action
	}

	// Collect form data
	data := make(map[string]string)
	collectFormData(form, data)

	if method == "GET" {
		// Append as query string
		var params []string
		for k, v := range data { params = append(params, k+"="+v) }
		if len(params) > 0 {
			sep := "?"
			if strings.Contains(action, "?") { sep = "&" }
			action += sep + strings.Join(params, "&")
		}
		return p.Navigate(action)
	}

	// POST
	var bodyParts []string
	for k, v := range data { bodyParts = append(bodyParts, k+"="+v) }
	bodyStr := strings.Join(bodyParts, "&")

	req, err := http.NewRequest("POST", action, strings.NewReader(bodyStr))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", p.UserAgent)
	for name, val := range p.Cookies { req.AddCookie(&http.Cookie{Name: name, Value: val}) }

	resp, err := p.Browser.Client.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()
	for _, cookie := range resp.Cookies() { p.Cookies[cookie.Name] = cookie.Value }
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))

	p.URL = action
	p.Doc = ParseHTML(string(body))
	p.setupJS()
	p.Scripts = ExtractScripts(p.Doc)
	p.executeScriptsWithTimeout(5 * time.Second)
	p.fireLifecycleEvents()
	return nil
}

func collectFormData(el *Element, data map[string]string) {
	if el.TagName == "INPUT" || el.TagName == "TEXTAREA" || el.TagName == "SELECT" {
		name := el.GetAttribute("name")
		if name != "" {
			val := el.GetAttribute("value")
			if val == "" && el.TagName == "TEXTAREA" {
				val = extractPlainText(el)
			}
			data[name] = val
		}
	}
	for _, child := range el.Children {
		if cel := nodeToElement(child); cel != nil {
			collectFormData(cel, data)
		}
	}
}

// simulateLazyLoad walks the DOM and copies lazy-load attributes to src.
// This simulates what lazy-load JS libraries do (swap data-src to src).
func simulateLazyLoad(doc *Document) {
	walkLazyLoad(&doc.Node)
}

func walkLazyLoad(n *Node) {
	if el := nodeToElement(n); el != nil && el.TagName == "IMG" {
		src := el.GetAttribute("src")
		if src == "" || strings.HasPrefix(src, "data:") || src == "about:blank" {
			// Try lazy-load attributes
			for _, attr := range []string{"data-src", "data-lazy-src", "data-original", "data-lazy"} {
				if val := el.GetAttribute(attr); val != "" {
					el.SetAttribute("src", val)
					break
				}
			}
		}
		// Also handle srcset
		if el.GetAttribute("src") == "" {
			if srcset := el.GetAttribute("data-srcset"); srcset != "" {
				el.SetAttribute("srcset", srcset)
			}
		}
	}
	// Also handle <source> inside <picture>
	if el := nodeToElement(n); el != nil && el.TagName == "SOURCE" {
		if el.GetAttribute("srcset") == "" {
			if ds := el.GetAttribute("data-srcset"); ds != "" {
				el.SetAttribute("srcset", ds)
			}
		}
	}
	for _, child := range n.Children {
		walkLazyLoad(child)
	}
}

// Eval executes JavaScript in the page context.
func (p *Page) Eval(code string) (*espresso.Value, error) {
	trimmed := strings.TrimSpace(code)
	for _, kw := range []string{"const ", "let ", "var ", "if ", "if(", "for ", "for(", "return ", "function ", "try ", "throw ", "class "} {
		if strings.HasPrefix(trimmed, kw) {
			return p.VM.Run(code)
		}
	}
	return p.VM.Eval(code)
}

// QuerySelector is a convenience method on Page.
func (p *Page) QuerySelector(selector string) *Element {
	if p.Doc == nil {
		return nil
	}
	return QuerySelector(&p.Doc.Node, selector)
}

// QuerySelectorAll is a convenience method on Page.
func (p *Page) QuerySelectorAll(selector string) []*Element {
	if p.Doc == nil {
		return nil
	}
	return QuerySelectorAll(&p.Doc.Node, selector)
}

// Title returns the page title.
func (p *Page) Title() string {
	if p.Doc == nil {
		return ""
	}
	return p.Doc.Title
}

// TextContent returns the text content of the first element matching selector.
func (p *Page) TextContent(selector string) string {
	el := p.QuerySelector(selector)
	if el == nil {
		return ""
	}
	var sb strings.Builder
	CollectTextFromElement(el, &sb)
	return sb.String()
}

// Close closes the page.
func (p *Page) Close() {
	for i, pg := range p.Browser.Pages {
		if pg == p {
			p.Browser.Pages = append(p.Browser.Pages[:i], p.Browser.Pages[i+1:]...)
			break
		}
	}
}

// Close closes the browser and all pages.
func (b *Browser) Close() {
	b.Pages = nil
}
