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
		VM:        espresso.New(),
		Cookies:   make(map[string]string),
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

	resp, err := p.Browser.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

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
	// Large frameworks — jQuery is shimmed, skip the rest
	"jquery.min.js", "jquery-ui",
	"bootstrap.min.js", "bootstrap.bundle",
	"popper.js", "popper.min.js",
	"leaflet.min.js", "leaflet-src",
	"slick.min.js", "slick.js",
	"react.production", "react-dom.production",
	"vue.min.js", "vue.runtime",
	"angular.min.js", "angular.js",
	"summernote", "ckeditor", "tinymce",
	"turnstile", "challenges.cloudflare",
}

func shouldSkipScript(src string) bool {
	lower := strings.ToLower(src)
	for _, domain := range skipScriptDomains {
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
			scriptTimeout = 1 * time.Second
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
