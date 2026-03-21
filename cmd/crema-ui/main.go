package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/marcoschwartz/crema"
)

var (
	browser *crema.Browser
	page    *crema.Page
	mu      sync.Mutex
)

func main() {
	port := "8899"
	proxy := ""

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--port":
			if i+1 < len(os.Args) { i++; port = os.Args[i] }
		case "--proxy":
			if i+1 < len(os.Args) { i++; proxy = os.Args[i] }
		}
	}

	if proxy != "" {
		browser = crema.NewBrowserWithProxy(proxy)
	} else {
		browser = crema.NewBrowser()
	}
	page = browser.NewPage()

	http.HandleFunc("/", serveUI)
	http.HandleFunc("/api/navigate", handleNavigate)
	http.HandleFunc("/api/screenshot", handleScreenshot)
	http.HandleFunc("/api/eval", handleEval)
	http.HandleFunc("/api/info", handleInfo)
	http.HandleFunc("/api/click", handleClick)
	http.HandleFunc("/api/click-at", handleClickAt)
	http.HandleFunc("/api/set-proxy", handleSetProxy)

	fmt.Printf("Crema UI running at http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, nil))
}

func handleNavigate(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		http.Error(w, "url required", 400)
		return
	}
	// Add https:// if no scheme
	if len(url) > 0 && url[0] != 'h' {
		url = "https://" + url
	}

	mu.Lock()
	defer mu.Unlock()

	// Recover from panics so the server stays up
	defer func() {
		if rec := recover(); rec != nil {
			writeJSON(w, map[string]interface{}{"error": fmt.Sprintf("crash: %v", rec)})
		}
	}()

	page = browser.NewPage()
	err := page.Navigate(url)
	if err != nil {
		writeJSON(w, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]interface{}{
		"ok":    true,
		"title": page.Title(),
		"url":   url,
		"links": len(page.QuerySelectorAll("a")),
	})
}

func handleScreenshot(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	defer func() {
		if rec := recover(); rec != nil {
			http.Error(w, fmt.Sprintf("crash: %v", rec), 500)
		}
	}()

	if page == nil || page.Doc == nil {
		http.Error(w, "no page loaded", 400)
		return
	}

	data, err := page.ScreenshotSize(1280, 900)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	w.Header().Set("Content-Type", "image/png")
	w.Write(data)
}

func handleEval(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "code required", 400)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	if page == nil {
		writeJSON(w, map[string]interface{}{"error": "no page loaded"})
		return
	}

	result, err := page.Eval(code)
	if err != nil {
		writeJSON(w, map[string]interface{}{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]interface{}{
		"result": result.String(),
	})
}

func handleInfo(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()

	if page == nil || page.Doc == nil {
		writeJSON(w, map[string]interface{}{"loaded": false})
		return
	}

	// Collect links
	links := page.QuerySelectorAll("a")
	linkData := make([]map[string]string, 0, len(links))
	for _, l := range links {
		href := l.GetAttribute("href")
		text := ""
		var sb strings.Builder
		crema.CollectTextFromElement(l, &sb)
		text = sb.String()
		if text == "" { text = href }
		if len(text) > 60 { text = text[:60] + "..." }
		linkData = append(linkData, map[string]string{"href": href, "text": text})
	}

	// Collect inputs
	inputs := page.QuerySelectorAll("input")
	inputData := make([]map[string]string, 0, len(inputs))
	for _, inp := range inputs {
		inputData = append(inputData, map[string]string{
			"type":        inp.GetAttribute("type"),
			"name":        inp.GetAttribute("name"),
			"id":          inp.GetAttribute("id"),
			"placeholder": inp.GetAttribute("placeholder"),
		})
	}

	// Screenshot as base64 for embedding
	ssData, _ := page.ScreenshotSize(1280, 900)
	ss64 := base64.StdEncoding.EncodeToString(ssData)

	writeJSON(w, map[string]interface{}{
		"loaded":     true,
		"title":      page.Title(),
		"url":        page.URL,
		"links":      linkData,
		"inputs":     inputData,
		"screenshot": ss64,
	})
}

func handleClick(w http.ResponseWriter, r *http.Request) {
	selector := r.URL.Query().Get("selector")
	if selector == "" {
		http.Error(w, "selector required", 400)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	el := page.QuerySelector(selector)
	if el == nil {
		writeJSON(w, map[string]interface{}{"error": "element not found"})
		return
	}

	// If it's a link, navigate to it
	if el.TagName == "A" {
		href := el.GetAttribute("href")
		if href != "" {
			page = browser.NewPage()
			err := page.Navigate(href)
			if err != nil {
				writeJSON(w, map[string]interface{}{"error": err.Error()})
				return
			}
			writeJSON(w, map[string]interface{}{
				"ok":       true,
				"navigated": href,
				"title":    page.Title(),
			})
			return
		}
	}

	writeJSON(w, map[string]interface{}{"ok": true, "clicked": selector})
}

func handleClickAt(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			writeJSON(w, map[string]interface{}{"error": fmt.Sprintf("crash: %v", rec)})
		}
	}()
	xStr := r.URL.Query().Get("x")
	yStr := r.URL.Query().Get("y")
	if xStr == "" || yStr == "" {
		http.Error(w, "x and y required", 400)
		return
	}
	var x, y int
	fmt.Sscanf(xStr, "%d", &x)
	fmt.Sscanf(yStr, "%d", &y)

	mu.Lock()
	defer mu.Unlock()

	if page == nil || page.LastLayout == nil {
		writeJSON(w, map[string]interface{}{"error": "no page loaded or no layout"})
		return
	}

	hit := crema.HitTestElement(page.LastLayout, x, y)
	if hit == nil || hit.Action == "none" {
		writeJSON(w, map[string]interface{}{
			"action": "none",
			"x": x, "y": y,
		})
		return
	}

	result := map[string]interface{}{
		"action": hit.Action,
		"x": x, "y": y,
		"text": hit.Text,
	}

	switch hit.Action {
	case "navigate":
		href := hit.Link
		if href == "" {
			result["error"] = "no href"
			break
		}
		// Resolve relative URLs
		if len(href) > 0 && href[0] == '/' {
			// Get origin from current URL
			origin := getOrigin(page.URL)
			href = origin + href
		}
		result["href"] = href

		// Navigate
		page = browser.NewPage()
		err := page.Navigate(href)
		if err != nil {
			result["error"] = err.Error()
		} else {
			result["title"] = page.Title()
			result["navigated"] = true
		}

	case "input":
		result["inputType"] = hit.InputType
		if hit.Element != nil {
			result["name"] = hit.Element.GetAttribute("name")
			result["id"] = hit.Element.ID
		}

	case "click":
		if hit.Element != nil {
			result["tag"] = hit.Element.TagName
		}
	}

	writeJSON(w, result)
}

func handleSetProxy(w http.ResponseWriter, r *http.Request) {
	proxyURL := r.URL.Query().Get("proxy")

	mu.Lock()
	defer mu.Unlock()

	if proxyURL == "" {
		// Clear proxy
		browser = crema.NewBrowser()
		page = browser.NewPage()
		writeJSON(w, map[string]interface{}{"ok": true, "proxy": ""})
		return
	}

	browser = crema.NewBrowserWithProxy(proxyURL)
	page = browser.NewPage()
	writeJSON(w, map[string]interface{}{"ok": true, "proxy": proxyURL})
}

func getOrigin(u string) string {
	idx := 0
	for i, c := range u {
		if c == '/' && i > 8 { idx = i; break }
	}
	if idx == 0 { return u }
	return u[:idx]
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func serveUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, uiHTML)
}

const uiHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Crema Browser</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; background: #1a1a2e; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }

  /* Top bar */
  .topbar { display: flex; align-items: center; gap: 8px; padding: 8px 12px; background: #16213e; border-bottom: 1px solid #0f3460; }
  .topbar .logo { font-weight: 700; font-size: 16px; color: #e94560; letter-spacing: 1px; }
  .topbar input[type=text] { flex: 1; padding: 8px 14px; border-radius: 20px; border: 1px solid #0f3460; background: #1a1a2e; color: #fff; font-size: 14px; outline: none; }
  .topbar input[type=text]:focus { border-color: #e94560; }
  .topbar button { padding: 8px 20px; border-radius: 20px; border: none; background: #e94560; color: white; font-weight: 600; cursor: pointer; font-size: 14px; }
  .topbar button:hover { background: #c73e54; }
  .topbar .proxy-input { width: 260px; padding: 6px 10px; border-radius: 12px; border: 1px solid #0f3460; background: #1a1a2e; color: #aaa; font-size: 11px; outline: none; }
  .topbar .proxy-input:focus { border-color: #e94560; color: #fff; }
  .topbar .proxy-input.active { border-color: #27ae60; color: #2ecc71; }
  .topbar .proxy-label { font-size: 11px; color: #666; }
  .topbar .status { font-size: 12px; color: #888; min-width: 80px; text-align: right; }

  /* Main area */
  .main { display: flex; flex: 1; overflow: hidden; }

  /* Screenshot panel */
  .screenshot-panel { flex: 1; overflow: auto; background: #111; display: flex; align-items: flex-start; justify-content: center; padding: 12px; }
  .screenshot-panel img { max-width: 100%; border: 1px solid #333; box-shadow: 0 4px 20px rgba(0,0,0,0.5); cursor: pointer; }
  .click-marker { position: absolute; width: 20px; height: 20px; border-radius: 50%; background: rgba(233,69,96,0.5); border: 2px solid #e94560; transform: translate(-50%,-50%); pointer-events: none; animation: clickPulse 0.5s ease-out; z-index: 10; }
  @keyframes clickPulse { 0% { transform: translate(-50%,-50%) scale(0); opacity: 1; } 100% { transform: translate(-50%,-50%) scale(2); opacity: 0; } }
  .tooltip { position: absolute; background: #16213e; color: #e0e0e0; padding: 4px 10px; border-radius: 4px; font-size: 12px; border: 1px solid #0f3460; pointer-events: none; z-index: 11; white-space: nowrap; }
  .screenshot-panel .empty { color: #555; font-size: 18px; margin-top: 200px; }

  /* Side panel */
  .sidepanel { width: 340px; background: #16213e; border-left: 1px solid #0f3460; display: flex; flex-direction: column; overflow: hidden; }
  .sidepanel h3 { padding: 10px 14px; font-size: 13px; text-transform: uppercase; letter-spacing: 1px; color: #e94560; border-bottom: 1px solid #0f3460; }

  /* Page info */
  .page-info { padding: 10px 14px; font-size: 13px; border-bottom: 1px solid #0f3460; }
  .page-info .title { font-weight: 600; font-size: 15px; color: #fff; margin-bottom: 4px; }
  .page-info .meta { color: #888; }

  /* Console */
  .console { border-top: 1px solid #0f3460; }
  .console-input { display: flex; padding: 8px; gap: 6px; }
  .console-input input { flex: 1; padding: 6px 10px; border-radius: 4px; border: 1px solid #0f3460; background: #1a1a2e; color: #0f0; font-family: monospace; font-size: 13px; }
  .console-output { padding: 8px 14px; font-family: monospace; font-size: 12px; color: #0f0; max-height: 120px; overflow-y: auto; background: #111; }

  /* Links list */
  .links-list { flex: 1; overflow-y: auto; padding: 6px; }
  .links-list a { display: block; padding: 4px 8px; color: #5dade2; text-decoration: none; font-size: 12px; border-radius: 3px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .links-list a:hover { background: #0f3460; }

  /* Loading overlay */
  .loading { position: fixed; top: 0; left: 0; right: 0; bottom: 0; background: rgba(0,0,0,0.6); display: flex; align-items: center; justify-content: center; z-index: 100; }
  .loading .spinner { width: 40px; height: 40px; border: 3px solid #333; border-top: 3px solid #e94560; border-radius: 50%; animation: spin 0.8s linear infinite; }
  @keyframes spin { to { transform: rotate(360deg); } }
  .hidden { display: none; }
</style>
</head>
<body>

<div class="topbar">
  <span class="logo">☕ CREMA</span>
  <input type="text" id="urlbar" placeholder="Enter URL..." value="https://example.com" onkeydown="if(event.key==='Enter')navigate()">
  <button onclick="navigate()">Go</button>
  <span class="proxy-label">Proxy:</span>
  <input type="text" class="proxy-input" id="proxyInput" placeholder="http://user:pass@proxy:8080" onkeydown="if(event.key==='Enter')setProxy()">
  <button onclick="setProxy()" style="padding:6px 12px;font-size:12px;border-radius:12px;">Set</button>
  <span class="status" id="status"></span>
</div>

<div class="main">
  <div class="screenshot-panel" id="ssPanel">
    <div class="empty" id="placeholder">Enter a URL to start browsing</div>
    <img id="screenshot" class="hidden" alt="Page screenshot" onclick="handleImageClick(event)">
  </div>

  <div class="sidepanel">
    <div class="page-info" id="pageInfo">
      <div class="title" id="pageTitle">No page loaded</div>
      <div class="meta" id="pageMeta"></div>
    </div>

    <h3>Console</h3>
    <div class="console">
      <div class="console-output" id="consoleOutput"></div>
      <div class="console-input">
        <input type="text" id="evalInput" placeholder="document.title" onkeydown="if(event.key==='Enter')evalJS()">
      </div>
    </div>

    <h3>Links (<span id="linkCount">0</span>)</h3>
    <div class="links-list" id="linksList"></div>
  </div>
</div>

<div class="loading hidden" id="loading"><div class="spinner"></div></div>

<script>
function showLoading() { document.getElementById('loading').classList.remove('hidden'); }
function hideLoading() { document.getElementById('loading').classList.add('hidden'); }
function setStatus(s) { document.getElementById('status').textContent = s; }

async function navigate() {
  const url = document.getElementById('urlbar').value.trim();
  if (!url) return;
  showLoading();
  setStatus('Loading...');
  const start = Date.now();

  try {
    const res = await fetch('/api/navigate?url=' + encodeURIComponent(url));
    const data = await res.json();
    const elapsed = Date.now() - start;

    if (data.error) {
      setStatus('Error');
      alert(data.error);
      hideLoading();
      return;
    }

    setStatus(elapsed + 'ms');
    document.getElementById('pageTitle').textContent = data.title || '(no title)';
    document.getElementById('pageMeta').textContent = data.links + ' links · ' + elapsed + 'ms';

    // Load screenshot
    const img = document.getElementById('screenshot');
    img.src = '/api/screenshot?t=' + Date.now();
    img.classList.remove('hidden');
    document.getElementById('placeholder').classList.add('hidden');

    // Load page info
    loadInfo();
  } catch(e) {
    setStatus('Error');
    alert(e.message);
  }
  hideLoading();
}

async function loadInfo() {
  try {
    const res = await fetch('/api/info');
    const data = await res.json();
    if (!data.loaded) return;

    // Links
    const list = document.getElementById('linksList');
    list.innerHTML = '';
    document.getElementById('linkCount').textContent = data.links.length;
    for (const link of data.links) {
      const a = document.createElement('a');
      a.href = '#';
      a.textContent = link.text || link.href;
      a.title = link.href;
      a.onclick = (e) => { e.preventDefault(); clickLink(link.href); };
      list.appendChild(a);
    }
  } catch(e) {}
}

async function clickLink(href) {
  if (!href || href === '#') return;
  document.getElementById('urlbar').value = href;
  navigate();
}

async function evalJS() {
  const input = document.getElementById('evalInput');
  const code = input.value.trim();
  if (!code) return;

  const output = document.getElementById('consoleOutput');

  try {
    const res = await fetch('/api/eval?code=' + encodeURIComponent(code));
    const data = await res.json();
    const line = document.createElement('div');
    line.textContent = '> ' + code + ' → ' + (data.result || data.error || 'undefined');
    output.appendChild(line);
    output.scrollTop = output.scrollHeight;
  } catch(e) {
    const line = document.createElement('div');
    line.textContent = '> ' + code + ' → ERROR: ' + e.message;
    line.style.color = '#f44';
    output.appendChild(line);
  }

  input.value = '';

  // Refresh screenshot after eval
  const img = document.getElementById('screenshot');
  img.src = '/api/screenshot?t=' + Date.now();
}

async function setProxy() {
  const input = document.getElementById('proxyInput');
  const proxyURL = input.value.trim();
  try {
    const res = await fetch('/api/set-proxy?proxy=' + encodeURIComponent(proxyURL));
    const data = await res.json();
    if (data.ok) {
      if (proxyURL) {
        input.classList.add('active');
        setStatus('Proxy set');
      } else {
        input.classList.remove('active');
        setStatus('Proxy cleared');
      }
    }
  } catch(e) {
    setStatus('Proxy error');
  }
}

async function handleImageClick(event) {
  const img = event.target;
  const rect = img.getBoundingClientRect();

  // Scale click coordinates to actual image size (1280px viewport)
  const scaleX = 1280 / rect.width;
  const scaleY = img.naturalHeight / rect.height;
  const x = Math.round((event.clientX - rect.left) * scaleX);
  const y = Math.round((event.clientY - rect.top) * scaleY);

  // Show click marker
  const marker = document.createElement('div');
  marker.className = 'click-marker';
  marker.style.left = event.clientX + 'px';
  marker.style.top = event.clientY + 'px';
  document.body.appendChild(marker);
  setTimeout(() => marker.remove(), 500);

  // Send click to server
  try {
    const res = await fetch('/api/click-at?x=' + x + '&y=' + y);
    const data = await res.json();

    const output = document.getElementById('consoleOutput');
    const line = document.createElement('div');
    line.textContent = 'click(' + x + ',' + y + ') → ' + data.action + (data.text ? ' "' + data.text + '"' : '') + (data.href ? ' → ' + data.href : '');
    line.style.color = data.action === 'navigate' ? '#5dade2' : data.action === 'input' ? '#f0c040' : '#888';
    output.appendChild(line);
    output.scrollTop = output.scrollHeight;

    if (data.navigated) {
      // Page navigated — refresh everything
      document.getElementById('urlbar').value = data.href || '';
      setStatus('Navigated');
      document.getElementById('pageTitle').textContent = data.title || '(no title)';
      img.src = '/api/screenshot?t=' + Date.now();
      loadInfo();
    }
  } catch(e) {
    console.error('click error:', e);
  }
}
</script>
</body>
</html>
`
