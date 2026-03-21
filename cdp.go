package crema

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// CDPServer serves the Chrome DevTools Protocol over WebSocket.
// This allows Puppeteer, Playwright, chromedp, and other CDP clients
// to use Crema as a drop-in replacement for headless Chrome.
type CDPServer struct {
	Browser    *Browser
	Port       int
	pages      map[string]*Page           // targetId → Page
	sessions   map[string]*Page           // sessionId → Page
	autoAttach map[*websocket.Conn]bool   // connections with autoAttach enabled
	mu         sync.RWMutex
	nextID     atomic.Int64
}

// NewCDPServer creates a CDP server backed by a Crema browser.
func NewCDPServer(browser *Browser, port int) *CDPServer {
	return &CDPServer{
		Browser:  browser,
		Port:     port,
		pages:      make(map[string]*Page),
		sessions:   make(map[string]*Page),
		autoAttach: make(map[*websocket.Conn]bool),
	}
}

// Start starts the CDP server (blocking).
func (s *CDPServer) Start() error {
	mux := http.NewServeMux()

	// Discovery endpoints (used by Puppeteer/Playwright to find targets)
	mux.HandleFunc("/json/version", s.handleVersion)
	mux.HandleFunc("/json", s.handleList)
	mux.HandleFunc("/json/list", s.handleList)
	mux.HandleFunc("/json/new", s.handleNew)
	mux.HandleFunc("/json/close/", s.handleClose)

	// WebSocket endpoint for CDP
	mux.HandleFunc("/devtools/page/", s.handleDevTools)
	mux.HandleFunc("/devtools/browser", s.handleDevToolsBrowser)

	addr := fmt.Sprintf(":%d", s.Port)
	log.Printf("Crema CDP server listening on %s", addr)
	return http.ListenAndServe(addr, mux)
}

// ─── HTTP Discovery Endpoints ───────────────────────────────

func (s *CDPServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	addr := r.Host
	resp := map[string]string{
		"Browser":              "Crema/1.0",
		"Protocol-Version":     "1.3",
		"User-Agent":           s.Browser.UserAgent,
		"V8-Version":           "0.0.0 (Espresso)",
		"WebKit-Version":       "0.0.0",
		"webSocketDebuggerUrl": fmt.Sprintf("ws://%s/devtools/browser", addr),
	}
	writeJSON(w, resp)
}

func (s *CDPServer) handleList(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var targets []map[string]string
	for id, page := range s.pages {
		targets = append(targets, s.targetInfo(id, page, r.Host))
	}

	// Create a default page if none exist
	if len(targets) == 0 {
		id, page := s.createPage()
		targets = append(targets, s.targetInfo(id, page, r.Host))
	}

	writeJSON(w, targets)
}

func (s *CDPServer) handleNew(w http.ResponseWriter, r *http.Request) {
	id, page := s.createPage()

	// Navigate if URL provided
	url := r.URL.Query().Get("url")
	if url == "" {
		url = r.FormValue("url")
	}
	if url != "" {
		page.Navigate(url)
	}

	writeJSON(w, s.targetInfo(id, page, r.Host))
}

func (s *CDPServer) handleClose(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 3 {
		http.Error(w, "missing target id", 400)
		return
	}
	targetID := parts[len(parts)-1]

	s.mu.Lock()
	if page, ok := s.pages[targetID]; ok {
		page.Close()
		delete(s.pages, targetID)
	}
	s.mu.Unlock()

	w.WriteHeader(200)
	fmt.Fprint(w, "Target is closing")
}

func (s *CDPServer) createPage() (string, *Page) {
	id := fmt.Sprintf("crema-%d", s.nextID.Add(1))
	page := s.Browser.NewPage()
	page.URL = "about:blank"

	s.mu.Lock()
	s.pages[id] = page
	s.mu.Unlock()

	return id, page
}

func (s *CDPServer) targetInfo(id string, page *Page, host string) map[string]string {
	title := ""
	if page.Doc != nil {
		title = page.Title()
	}
	return map[string]string{
		"id":                    id,
		"type":                  "page",
		"title":                 title,
		"url":                   page.URL,
		"webSocketDebuggerUrl":  fmt.Sprintf("ws://%s/devtools/page/%s", host, id),
		"devtoolsFrontendUrl":   "",
	}
}

// ─── WebSocket CDP Handler ──────────────────────────────────

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *CDPServer) handleDevToolsBrowser(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		s.handleBrowserMessage(conn, msg)
	}
}

func (s *CDPServer) handleDevTools(w http.ResponseWriter, r *http.Request) {
	// Extract target ID from path
	parts := strings.Split(r.URL.Path, "/")
	targetID := parts[len(parts)-1]

	s.mu.RLock()
	page, ok := s.pages[targetID]
	s.mu.RUnlock()

	if !ok {
		// Auto-create page
		var id string
		id, page = s.createPage()
		targetID = id
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		s.handlePageMessage(conn, page, targetID, msg)
	}
}

// ─── CDP Message Handling ───────────────────────────────────

type cdpMessage struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
}

type cdpResponse struct {
	ID        int64       `json:"id"`
	Result    interface{} `json:"result"`
	SessionID string      `json:"sessionId,omitempty"`
}

type cdpError struct {
	ID    int64 `json:"id"`
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type cdpEvent struct {
	Method    string      `json:"method"`
	Params    interface{} `json:"params"`
	SessionID string      `json:"sessionId,omitempty"`
}

func (s *CDPServer) handleBrowserMessage(conn *websocket.Conn, raw []byte) {
	var msg cdpMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	log.Printf("CDP browser ← %s (id=%d)", msg.Method, msg.ID)

	switch msg.Method {
	case "Target.getTargets":
		var targets []map[string]interface{}
		s.mu.RLock()
		for id, page := range s.pages {
			targets = append(targets, map[string]interface{}{
				"targetId": id,
				"type":     "page",
				"title":    page.Title(),
				"url":      page.URL,
			})
		}
		s.mu.RUnlock()
		sendResult(conn, msg.ID, map[string]interface{}{"targetInfos": targets})

	case "Target.createTarget":
		var params struct{ URL string `json:"url"` }
		json.Unmarshal(msg.Params, &params)
		id, page := s.createPage()
		if params.URL != "" && params.URL != "about:blank" {
			page.Navigate(params.URL)
		}
		sendResult(conn, msg.ID, map[string]string{"targetId": id})

		// If autoAttach is on, automatically attach and emit events
		s.mu.RLock()
		autoAttach := s.autoAttach[conn]
		s.mu.RUnlock()

		if autoAttach {
			sessionID := "session-" + id
			s.mu.Lock()
			s.sessions[sessionID] = page
			s.mu.Unlock()

			sendEvent(conn, "", "Target.targetCreated", map[string]interface{}{
				"targetInfo": map[string]interface{}{
					"targetId":         id,
					"type":             "page",
					"title":            page.Title(),
					"url":              page.URL,
					"attached":         true,
					"browserContextId": "default",
				},
			})
			sendEvent(conn, "", "Target.attachedToTarget", map[string]interface{}{
				"sessionId": sessionID,
				"targetInfo": map[string]interface{}{
					"targetId":         id,
					"type":             "page",
					"title":            page.Title(),
					"url":              page.URL,
					"attached":         true,
					"browserContextId": "default",
				},
				"waitingForDebugger": false,
			})
		} else {
			sendEvent(conn, "", "Target.targetCreated", map[string]interface{}{
				"targetInfo": map[string]interface{}{
					"targetId":         id,
					"type":             "page",
					"title":            page.Title(),
					"url":              page.URL,
					"attached":         false,
					"browserContextId": "default",
				},
			})
		}

	case "Target.attachToTarget":
		var params struct {
			TargetID string `json:"targetId"`
			Flatten  bool   `json:"flatten"`
		}
		json.Unmarshal(msg.Params, &params)
		sessionID := "session-" + params.TargetID
		s.mu.Lock()
		if page, ok := s.pages[params.TargetID]; ok {
			s.sessions[sessionID] = page
		}
		s.mu.Unlock()
		sendResult(conn, msg.ID, map[string]string{"sessionId": sessionID})
		// Emit attachedToTarget event
		s.mu.RLock()
		page := s.pages[params.TargetID]
		pageURL := "about:blank"
		pageTitle := ""
		if page != nil {
			pageURL = page.URL
			pageTitle = page.Title()
		}
		s.mu.RUnlock()
		sendEvent(conn, "", "Target.attachedToTarget", map[string]interface{}{
			"sessionId": sessionID,
			"targetInfo": map[string]interface{}{
				"targetId":         params.TargetID,
				"type":             "page",
				"title":            pageTitle,
				"url":              pageURL,
				"attached":         true,
				"browserContextId": "default",
			},
			"waitingForDebugger": false,
		})

	case "Target.getBrowserContexts":
		sendResult(conn, msg.ID, map[string]interface{}{
			"browserContextIds": []string{},
		})

	case "Target.setDiscoverTargets":
		sendResult(conn, msg.ID, map[string]interface{}{})
		// Send existing targets as events
		s.mu.RLock()
		for id, page := range s.pages {
			sendEvent(conn, "", "Target.targetCreated", map[string]interface{}{
				"targetInfo": map[string]interface{}{
					"targetId":         id,
					"type":             "page",
					"title":            page.Title(),
					"url":              page.URL,
					"attached":         false,
					"browserContextId": "default",
				},
			})
		}
		s.mu.RUnlock()

	case "Target.setAutoAttach":
		s.mu.Lock()
		s.autoAttach[conn] = true
		s.mu.Unlock()
		sendResult(conn, msg.ID, map[string]interface{}{})

	case "Target.closeTarget":
		var params struct{ TargetID string `json:"targetId"` }
		json.Unmarshal(msg.Params, &params)
		s.mu.Lock()
		if page, ok := s.pages[params.TargetID]; ok {
			page.Close()
			delete(s.pages, params.TargetID)
		}
		s.mu.Unlock()
		sendResult(conn, msg.ID, map[string]bool{"success": true})

	default:
		// If has sessionId, route to page handler
		if msg.SessionID != "" {
			s.mu.RLock()
			page, ok := s.sessions[msg.SessionID]
			s.mu.RUnlock()
			if ok {
				s.handlePageMessage(conn, page, msg.SessionID, raw)
				return
			}
		}
		sendResult(conn, msg.ID, map[string]interface{}{})
	}
}

func (s *CDPServer) handlePageMessage(conn *websocket.Conn, page *Page, sessionID string, raw []byte) {
	var msg cdpMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	log.Printf("CDP page ← %s (id=%d session=%s)", msg.Method, msg.ID, msg.SessionID)

	// Use the sessionId from the message for responses
	respSessionID := msg.SessionID

	// Helper to send result with correct sessionId
	reply := func(result interface{}) {
		if respSessionID != "" {
			sendResultSession(conn, msg.ID, respSessionID, result)
		} else {
			sendResult(conn, msg.ID, result)
		}
	}
	replyEvent := func(method string, params interface{}) {
		sendEvent(conn, respSessionID, method, params)
	}
	_ = replyEvent

	switch msg.Method {

	// ── Page Domain ─────────────────────────────────────
	case "Page.enable":
		reply(map[string]interface{}{})

	case "Page.getFrameTree":
		reply(map[string]interface{}{
			"frameTree": map[string]interface{}{
				"frame": map[string]interface{}{
					"id":             "main-frame",
					"loaderId":       "loader-1",
					"url":            page.URL,
					"securityOrigin": page.URL,
					"mimeType":       "text/html",
				},
				"childFrames": []interface{}{},
			},
		})

	case "Page.setLifecycleEventsEnabled":
		reply(map[string]interface{}{})

	case "Page.navigate":
		var params struct {
			URL string `json:"url"`
		}
		json.Unmarshal(msg.Params, &params)
		frameID := "main-frame"
		loaderID := fmt.Sprintf("loader-%d", s.nextID.Add(1))

		// Send navigate response immediately, then fetch async
		reply(map[string]string{
			"frameId":  frameID,
			"loaderId": loaderID,
		})

		// Navigate and emit events in goroutine so we don't block the websocket loop
		go func() {
			log.Printf("CDP navigate goroutine: fetching %s", params.URL)
			err := page.Navigate(params.URL)
			if err != nil {
				log.Printf("CDP navigate error: %v", err)
				return
			}
			log.Printf("CDP navigate goroutine: done, emitting events")
			// Emit lifecycle events that Puppeteer waits for
			replyEvent("Page.lifecycleEvent", map[string]interface{}{
				"frameId": frameID, "loaderId": loaderID, "name": "init", "timestamp": 0,
			})
			replyEvent("Page.frameNavigated", map[string]interface{}{
				"frame": map[string]interface{}{
					"id": frameID, "url": params.URL, "loaderId": loaderID,
					"securityOrigin": params.URL, "mimeType": "text/html",
				},
			})
			replyEvent("Page.lifecycleEvent", map[string]interface{}{
				"frameId": frameID, "loaderId": loaderID, "name": "commit", "timestamp": 0,
			})
			replyEvent("Page.lifecycleEvent", map[string]interface{}{
				"frameId": frameID, "loaderId": loaderID, "name": "DOMContentLoaded", "timestamp": 0,
			})
			replyEvent("Page.lifecycleEvent", map[string]interface{}{
				"frameId": frameID, "loaderId": loaderID, "name": "load", "timestamp": 0,
			})
			replyEvent("Page.loadEventFired", map[string]interface{}{
				"timestamp": 0,
			})
			replyEvent("Page.lifecycleEvent", map[string]interface{}{
				"frameId": frameID, "loaderId": loaderID, "name": "networkIdle", "timestamp": 0,
			})
			// Emit new execution context for the navigated page
			replyEvent("Runtime.executionContextsCleared", map[string]interface{}{})
			replyEvent("Runtime.executionContextCreated", map[string]interface{}{
				"context": map[string]interface{}{
					"id":     1,
					"origin": params.URL,
					"name":   "",
					"auxData": map[string]interface{}{
						"isDefault": true,
						"type":      "default",
						"frameId":   frameID,
					},
				},
			})
		}()

	case "Page.captureScreenshot":
		var params struct {
			Format  string `json:"format"`
			Quality int    `json:"quality"`
		}
		json.Unmarshal(msg.Params, &params)
		if params.Format == "" {
			params.Format = "png"
		}
		data, err := page.Screenshot()
		if err != nil {
			sendError(conn, msg.ID, err.Error())
			return
		}
		reply(map[string]string{
			"data": base64.StdEncoding.EncodeToString(data),
		})

	case "Page.getLayoutMetrics":
		reply(map[string]interface{}{
			"layoutViewport": map[string]int{"pageX": 0, "pageY": 0, "clientWidth": 1280, "clientHeight": 800},
			"visualViewport": map[string]interface{}{"offsetX": 0, "offsetY": 0, "pageX": 0, "pageY": 0, "clientWidth": 1280, "clientHeight": 800, "scale": 1},
			"contentSize":    map[string]int{"x": 0, "y": 0, "width": 1280, "height": 800},
		})

	// ── Runtime Domain ──────────────────────────────────
	case "Runtime.enable":
		reply(map[string]interface{}{})
		// Emit execution context created (required by Puppeteer)
		replyEvent("Runtime.executionContextCreated", map[string]interface{}{
			"context": map[string]interface{}{
				"id":   1,
				"origin": page.URL,
				"name": "",
			},
		})

	case "Runtime.runIfWaitingForDebugger":
		reply(map[string]interface{}{})

	case "Performance.enable", "Log.enable", "Target.setAutoAttach":
		reply(map[string]interface{}{})

	case "Runtime.callFunctionOn":
		var params struct {
			FunctionDeclaration string `json:"functionDeclaration"`
			ReturnByValue       bool   `json:"returnByValue"`
			ObjectID            string `json:"objectId"`
			ExecutionContextID  int    `json:"executionContextId"`
		}
		json.Unmarshal(msg.Params, &params)
		// Puppeteer sends function declarations like:
		//   () => document.title
		//   function() { return document.title; }
		//   () => { return ... }
		code := strings.TrimSpace(params.FunctionDeclaration)
		// Arrow function: () => expr
		if strings.HasPrefix(code, "()") || strings.HasPrefix(code, "() =>") {
			code = strings.TrimPrefix(code, "()")
			code = strings.TrimSpace(code)
			code = strings.TrimPrefix(code, "=>")
			code = strings.TrimSpace(code)
		}
		// function() { ... } — extract body
		if strings.HasPrefix(code, "function") {
			if idx := strings.Index(code, "{"); idx >= 0 {
				end := strings.LastIndex(code, "}")
				if end > idx {
					code = code[idx+1 : end]
				}
			}
		}
		result, _ := page.Eval(code)
		reply(map[string]interface{}{
			"result": espressoToRemoteObject(result),
		})

	case "Runtime.evaluate":
		var params struct {
			Expression    string `json:"expression"`
			ReturnByValue bool   `json:"returnByValue"`
		}
		json.Unmarshal(msg.Params, &params)
		result, err := page.Eval(params.Expression)
		if err != nil {
			sendError(conn, msg.ID, err.Error())
			return
		}
		reply(map[string]interface{}{
			"result": espressoToRemoteObject(result),
		})

	// ── DOM Domain ──────────────────────────────────────
	case "DOM.enable":
		reply(map[string]interface{}{})

	case "DOM.getDocument":
		reply(map[string]interface{}{
			"root": map[string]interface{}{
				"nodeId":    1,
				"nodeType":  9,
				"nodeName":  "#document",
				"childNodeCount": len(page.Doc.Children),
			},
		})

	case "DOM.querySelector":
		var params struct {
			NodeID   int    `json:"nodeId"`
			Selector string `json:"selector"`
		}
		json.Unmarshal(msg.Params, &params)
		el := page.QuerySelector(params.Selector)
		nodeID := 0
		if el != nil {
			nodeID = int(s.nextID.Add(1))
		}
		reply(map[string]int{"nodeId": nodeID})

	case "DOM.querySelectorAll":
		var params struct {
			NodeID   int    `json:"nodeId"`
			Selector string `json:"selector"`
		}
		json.Unmarshal(msg.Params, &params)
		elements := page.QuerySelectorAll(params.Selector)
		nodeIDs := make([]int, len(elements))
		for i := range elements {
			nodeIDs[i] = int(s.nextID.Add(1))
		}
		reply(map[string]interface{}{"nodeIds": nodeIDs})

	// ── Network Domain ──────────────────────────────────
	case "Network.enable":
		reply(map[string]interface{}{})

	case "Network.setExtraHTTPHeaders":
		reply(map[string]interface{}{})

	case "Network.setCookie":
		reply(map[string]bool{"success": true})

	// ── Input Domain ────────────────────────────────────
	case "Input.dispatchMouseEvent":
		var params struct {
			Type   string `json:"type"`
			X      int    `json:"x"`
			Y      int    `json:"y"`
			Button string `json:"button"`
		}
		json.Unmarshal(msg.Params, &params)

		// Only act on mousePressed (not moved/released)
		if params.Type == "mousePressed" && page.LastLayout != nil {
			hit := HitTestElement(page.LastLayout, params.X, params.Y)
			if hit != nil && hit.Action == "navigate" && hit.Link != "" {
				href := hit.Link
				// Resolve relative URLs
				if len(href) > 0 && href[0] == '/' {
					if idx := strings.Index(page.URL, "://"); idx > 0 {
						rest := page.URL[idx+3:]
						if si := strings.Index(rest, "/"); si > 0 {
							href = page.URL[:idx+3+si] + href
						}
					}
				}
				// Navigate in background
				go func() {
					page.Navigate(href)
					frameID := "main-frame"
					loaderID := fmt.Sprintf("loader-%d", s.nextID.Add(1))
					replyEvent("Page.frameNavigated", map[string]interface{}{
						"frame": map[string]interface{}{
							"id": frameID, "url": href, "loaderId": loaderID,
							"securityOrigin": href, "mimeType": "text/html",
						},
					})
					replyEvent("Page.lifecycleEvent", map[string]interface{}{
						"frameId": frameID, "loaderId": loaderID, "name": "load", "timestamp": 0,
					})
					replyEvent("Page.loadEventFired", map[string]interface{}{"timestamp": 0})
					replyEvent("Runtime.executionContextsCleared", map[string]interface{}{})
					replyEvent("Runtime.executionContextCreated", map[string]interface{}{
						"context": map[string]interface{}{
							"id": 1, "origin": href, "name": "",
							"auxData": map[string]interface{}{"isDefault": true, "type": "default", "frameId": frameID},
						},
					})
				}()
			}
		}
		reply(map[string]interface{}{})

	case "Input.dispatchKeyEvent":
		var params struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Key  string `json:"key"`
		}
		json.Unmarshal(msg.Params, &params)
		// For key events, we could update focused input value
		// For now, handle Enter key as form submit
		if params.Key == "Enter" && params.Type == "keyDown" {
			// Could trigger form submission
		}
		reply(map[string]interface{}{})

	case "Input.insertText":
		var params struct {
			Text string `json:"text"`
		}
		json.Unmarshal(msg.Params, &params)
		// Insert text into the focused element via JS
		if params.Text != "" {
			page.Eval(fmt.Sprintf(`
				if (document.activeElement && document.activeElement.tagName === 'INPUT') {
					document.activeElement.value = (document.activeElement.value || '') + '%s';
				}
			`, strings.ReplaceAll(params.Text, "'", "\\'")))
		}
		reply(map[string]interface{}{})

	// ── Emulation Domain ────────────────────────────────
	case "Emulation.setDeviceMetricsOverride":
		reply(map[string]interface{}{})

	case "Emulation.setUserAgentOverride":
		var params struct {
			UserAgent string `json:"userAgent"`
		}
		json.Unmarshal(msg.Params, &params)
		if params.UserAgent != "" {
			page.UserAgent = params.UserAgent
		}
		reply(map[string]interface{}{})

	// ── Browser Domain ──────────────────────────────────
	case "Browser.getVersion":
		reply(map[string]string{
			"protocolVersion": "1.3",
			"product":         "Crema/1.0",
			"userAgent":       page.UserAgent,
			"jsVersion":       "Espresso",
		})

	case "Page.addScriptToEvaluateOnNewDocument":
		reply(map[string]interface{}{"identifier": "1"})

	case "Page.createIsolatedWorld":
		var params struct {
			FrameID string `json:"frameId"`
			WorldName string `json:"worldName"`
		}
		json.Unmarshal(msg.Params, &params)
		ctxID := int(s.nextID.Add(1)) + 100
		reply(map[string]interface{}{"executionContextId": ctxID})
		// Emit context for the isolated world
		replyEvent("Runtime.executionContextCreated", map[string]interface{}{
			"context": map[string]interface{}{
				"id":     ctxID,
				"origin": page.URL,
				"name":   params.WorldName,
				"auxData": map[string]interface{}{
					"isDefault": false,
					"type":      "isolated",
					"frameId":   params.FrameID,
				},
			},
		})

	// ── Catch-all ───────────────────────────────────────
	default:
		// Return empty result for unknown methods (graceful degradation)
		reply(map[string]interface{}{})
	}
}

// ─── Helpers ────────────────────────────────────────────────

func espressoToRemoteObject(v interface{}) map[string]interface{} {
	if v == nil {
		return map[string]interface{}{"type": "undefined"}
	}

	type typer interface{ Type() int }
	type stringer interface{ String() string }
	type numberer interface{ Number() float64 }
	type booler interface{ Bool() bool }

	t, hasType := v.(typer)
	if !hasType {
		if s, ok := v.(stringer); ok {
			return map[string]interface{}{"type": "string", "value": s.String()}
		}
		return map[string]interface{}{"type": "undefined"}
	}

	switch t.Type() {
	case 0: // undefined
		return map[string]interface{}{"type": "undefined"}
	case 1: // null
		return map[string]interface{}{"type": "object", "subtype": "null", "value": nil}
	case 2: // bool
		val := false
		if b, ok := v.(booler); ok { val = b.Bool() }
		return map[string]interface{}{"type": "boolean", "value": val}
	case 3: // number
		val := 0.0
		if n, ok := v.(numberer); ok { val = n.Number() }
		return map[string]interface{}{"type": "number", "value": val, "description": fmt.Sprintf("%v", val)}
	case 4: // string
		val := ""
		if s, ok := v.(stringer); ok { val = s.String() }
		return map[string]interface{}{"type": "string", "value": val}
	case 5: // array
		return map[string]interface{}{"type": "object", "subtype": "array", "className": "Array", "description": "Array", "objectId": fmt.Sprintf("obj-%d", cdpObjCounter.Add(1))}
	case 6: // object
		desc := "Object"
		if s, ok := v.(stringer); ok { desc = s.String() }
		return map[string]interface{}{"type": "object", "className": "Object", "description": desc, "objectId": fmt.Sprintf("obj-%d", cdpObjCounter.Add(1))}
	default:
		if s, ok := v.(stringer); ok {
			return map[string]interface{}{"type": "string", "value": s.String()}
		}
		return map[string]interface{}{"type": "undefined"}
	}
}

var (
	wsMu          sync.Mutex
	cdpObjCounter atomic.Int64
)

func sendResult(conn *websocket.Conn, id int64, result interface{}) {
	resp := cdpResponse{ID: id, Result: result}
	data, _ := json.Marshal(resp)
	wsMu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	wsMu.Unlock()
}

func sendResultSession(conn *websocket.Conn, id int64, sessionID string, result interface{}) {
	resp := cdpResponse{ID: id, Result: result, SessionID: sessionID}
	data, _ := json.Marshal(resp)
	wsMu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	wsMu.Unlock()
}

func sendError(conn *websocket.Conn, id int64, message string) {
	resp := cdpError{ID: id}
	resp.Error.Code = -32000
	resp.Error.Message = message
	data, _ := json.Marshal(resp)
	wsMu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	wsMu.Unlock()
}

func sendEvent(conn *websocket.Conn, sessionID string, method string, params interface{}) {
	evt := cdpEvent{Method: method, Params: params, SessionID: sessionID}
	data, _ := json.Marshal(evt)
	wsMu.Lock()
	conn.WriteMessage(websocket.TextMessage, data)
	wsMu.Unlock()
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ensure strconv is used
var _ = strconv.Itoa
