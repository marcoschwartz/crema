package crema

import (
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/marcoschwartz/espresso"
)

// RegisterWebAPIs injects browser globals (window, fetch, setTimeout, etc.) into a VM.
func RegisterWebAPIs(vm *espresso.VM, page *Page) {
	// ─── console (already in espresso, but ensure it exists) ───

	// ─── setTimeout / setInterval ───────────────────────────
	vm.Set("setTimeout", espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 1 {
			return espresso.Undefined
		}
		callback := args[0]
		delay := 0.0
		if len(args) > 1 {
			delay = args[1].Number()
		}
		// For headless browsing, execute synchronously after delay
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		callFuncFromJS(callback, nil, vm)
		return espresso.NewNum(1) // timer id
	}))

	vm.Set("setInterval", espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		// No-op for headless — return a fake timer ID
		return espresso.NewNum(0)
	}))

	vm.Set("clearTimeout", espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	}))

	vm.Set("clearInterval", espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		return espresso.Undefined
	}))

	// ─── fetch() ────────────────────────────────────────────
	vm.Set("fetch", espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) == 0 {
			return espresso.MakeRejectedPromise(espresso.NewStr("fetch requires a URL"))
		}
		url := args[0].String()

		// Parse options
		method := "GET"
		var reqBody string
		headers := map[string]string{}
		if len(args) > 1 && args[1].IsObject() {
			opts := args[1]
			if m := opts.Get("method"); !m.IsUndefined() {
				method = strings.ToUpper(m.String())
			}
			if b := opts.Get("body"); !b.IsUndefined() {
				reqBody = b.String()
			}
			if h := opts.Get("headers"); h.IsObject() {
				for k, v := range h.Object() {
					headers[k] = v.String()
				}
			}
		}

		// Make the request
		var bodyReader io.Reader
		if reqBody != "" {
			bodyReader = strings.NewReader(reqBody)
		}
		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return espresso.MakeRejectedPromise(espresso.NewStr(err.Error()))
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		if page != nil && page.UserAgent != "" {
			req.Header.Set("User-Agent", page.UserAgent)
		}

		client := &http.Client{Timeout: 30 * time.Second, Transport: ChromeTransport()}
		resp, err := client.Do(req)
		if err != nil {
			return espresso.MakeRejectedPromise(espresso.NewStr(err.Error()))
		}

		// Build response object
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		bodyStr := string(respBody)

		respObj := espresso.NewObj(map[string]*espresso.Value{
			"ok":         espresso.NewBool(resp.StatusCode >= 200 && resp.StatusCode < 300),
			"status":     espresso.NewNum(float64(resp.StatusCode)),
			"statusText": espresso.NewStr(resp.Status),
			"url":        espresso.NewStr(url),
		})

		// .text() → Promise<string>
		respObj.Object()["text"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
			return espresso.MakeResolvedPromise(espresso.NewStr(bodyStr))
		})

		// .json() → Promise<object>
		respObj.Object()["json"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
			vm2 := espresso.New()
			parsed, _ := vm2.Eval("JSON.parse('" + escapeJSONString(bodyStr) + "')")
			return espresso.MakeResolvedPromise(parsed)
		})

		// .headers.get()
		headersObj := espresso.NewObj(map[string]*espresso.Value{})
		headersObj.Object()["get"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
			if len(args) == 0 {
				return espresso.Null
			}
			val := resp.Header.Get(args[0].String())
			if val == "" {
				return espresso.Null
			}
			return espresso.NewStr(val)
		})
		respObj.Object()["headers"] = headersObj

		return espresso.MakeResolvedPromise(respObj)
	}))

	// ─── window object ──────────────────────────────────────
	window := espresso.NewObj(map[string]*espresso.Value{})
	window.DefineGetter("location", func(args []*espresso.Value) *espresso.Value {
		loc := espresso.NewObj(map[string]*espresso.Value{
			"href":     espresso.NewStr(page.URL),
			"origin":   espresso.NewStr(extractOrigin(page.URL)),
			"protocol": espresso.NewStr(extractProtocol(page.URL)),
			"host":     espresso.NewStr(extractHost(page.URL)),
			"hostname": espresso.NewStr(extractHostname(page.URL)),
			"pathname": espresso.NewStr(extractPathname(page.URL)),
		})
		return loc
	})
	window.DefineGetter("innerWidth", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewNum(1920)
	})
	window.DefineGetter("innerHeight", func(args []*espresso.Value) *espresso.Value {
		return espresso.NewNum(1080)
	})
	window.DefineGetter("navigator", func(args []*espresso.Value) *espresso.Value {
		return buildNavigator(page)
	})
	// addEventListener / removeEventListener on window
	windowListeners := map[string][]*espresso.Value{}
	window.Object()["addEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		windowListeners[event] = append(windowListeners[event], cb)
		return espresso.Undefined
	})
	window.Object()["removeEventListener"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
		if len(args) < 2 {
			return espresso.Undefined
		}
		event := args[0].String()
		cb := args[1]
		listeners := windowListeners[event]
		for i, l := range listeners {
			if l == cb {
				windowListeners[event] = append(listeners[:i], listeners[i+1:]...)
				break
			}
		}
		return espresso.Undefined
	})
	window.Object()["dispatchEvent"] = espresso.NewNativeFunc(func(args []*espresso.Value) *espresso.Value {
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
		for i, cb := range windowListeners[eventType] {
			dispatchCallback(cb, eventObj, i)
		}
		return espresso.NewBool(true)
	})

	window.Object()["setTimeout"] = vm.Get("setTimeout")
	window.Object()["setInterval"] = vm.Get("setInterval")
	window.Object()["clearTimeout"] = vm.Get("clearTimeout")
	window.Object()["clearInterval"] = vm.Get("clearInterval")
	window.Object()["fetch"] = vm.Get("fetch")
	vm.SetValue("window", window)

	// ─── navigator ──────────────────────────────────────────
	vm.SetValue("navigator", buildNavigator(page))

	// ─── location ───────────────────────────────────────────
	vm.Set("location", map[string]interface{}{
		"href":     page.URL,
		"origin":   extractOrigin(page.URL),
		"protocol": extractProtocol(page.URL),
		"host":     extractHost(page.URL),
		"hostname": extractHostname(page.URL),
		"pathname": extractPathname(page.URL),
	})
}

func buildNavigator(page *Page) *espresso.Value {
	ua := "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
	if page != nil && page.UserAgent != "" {
		ua = page.UserAgent
	}
	return espresso.NewObj(map[string]*espresso.Value{
		"userAgent":  espresso.NewStr(ua),
		"language":   espresso.NewStr("en-US"),
		"languages":  espresso.NewArr([]*espresso.Value{espresso.NewStr("en-US"), espresso.NewStr("en")}),
		"platform":   espresso.NewStr("Linux x86_64"),
		"webdriver":  espresso.NewBool(false),
		"cookieEnabled": espresso.NewBool(true),
		"onLine":     espresso.NewBool(true),
	})
}

// ─── Helpers ────────────────────────────────────────────────

func callFuncFromJS(fn *espresso.Value, args []*espresso.Value, vm *espresso.VM) {
	if fn == nil {
		return
	}
	if fn.Type() == espresso.TypeFunc {
		vm.SetValue("__cb", fn)
		vm.Call("__cb")
	}
}

func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func extractOrigin(u string) string {
	idx := strings.Index(u, "://")
	if idx < 0 { return u }
	rest := u[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 { return u }
	return u[:idx+3+slashIdx]
}

func extractProtocol(u string) string {
	idx := strings.Index(u, "://")
	if idx < 0 { return "" }
	return u[:idx+1] // "https:"
}

func extractHost(u string) string {
	idx := strings.Index(u, "://")
	if idx < 0 { return u }
	rest := u[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 { return rest }
	return rest[:slashIdx]
}

func extractHostname(u string) string {
	host := extractHost(u)
	colonIdx := strings.LastIndex(host, ":")
	if colonIdx < 0 { return host }
	return host[:colonIdx]
}

func extractPathname(u string) string {
	idx := strings.Index(u, "://")
	if idx < 0 { return "/" }
	rest := u[idx+3:]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 { return "/" }
	return rest[slashIdx:]
}
