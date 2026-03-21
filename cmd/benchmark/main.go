package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/marcoschwartz/crema"
)

var urls = []string{
	"https://example.com",
	"https://news.ycombinator.com",
	"https://httpbin.org/html",
	"https://jsonplaceholder.typicode.com",
}

type navResult struct {
	URL        string  `json:"url"`
	NavMs      float64 `json:"nav_ms"`
	ScreenMs   float64 `json:"screen_ms"`
	Title      string  `json:"title"`
	Links      int     `json:"links"`
}

func main() {
	fmt.Println("╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                 Crema vs Chrome Headless Benchmark                  ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	// ── Disk Usage ──────────────────────────────────────
	cremaDisk := getDockerImageSize("crema:latest")
	chromeDisk := getDockerImageSize("chromedp/headless-shell:latest")

	fmt.Println("┌─ Disk Usage ────────────────────────────────────────────────────────┐")
	fmt.Printf("│  Crema             %7.1f MB                                       │\n", cremaDisk)
	fmt.Printf("│  Chrome headless   %7.1f MB                                       │\n", chromeDisk)
	if chromeDisk > 0 {
		fmt.Printf("│  → Crema is %.0fx smaller                                           │\n", chromeDisk/cremaDisk)
	}
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// ── Start Chrome ────────────────────────────────────
	fmt.Println("Starting Chrome headless...")
	chromeStartT := time.Now()
	containerID := startChrome()
	chromeStartMs := time.Since(chromeStartT).Seconds() * 1000
	if containerID == "" {
		fmt.Println("  Chrome failed to start, skipping Chrome benchmarks")
		runCremaOnly()
		return
	}
	defer exec.Command("docker", "rm", "-f", containerID).Run()
	fmt.Printf("  Chrome started in %.0f ms (container: %s)\n\n", chromeStartMs, containerID[:12])

	// ── Crema startup ───────────────────────────────────
	cremaStartT := time.Now()
	browser := crema.NewBrowser()
	_ = browser.NewPage()
	cremaStartMs := time.Since(cremaStartT).Seconds() * 1000
	browser.Close()

	fmt.Println("┌─ Startup Time ──────────────────────────────────────────────────────┐")
	fmt.Printf("│  Crema             %7.1f ms                                       │\n", cremaStartMs)
	fmt.Printf("│  Chrome            %7.0f ms                                       │\n", chromeStartMs)
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// ── Memory ──────────────────────────────────────────
	browser = crema.NewBrowser()
	var memBefore, memAfter runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memBefore)
	p := browser.NewPage()
	p.Navigate("https://example.com")
	runtime.GC()
	runtime.ReadMemStats(&memAfter)
	cremaPageMB := float64(memAfter.Alloc-memBefore.Alloc) / 1024 / 1024
	cremaTotalMB := float64(memAfter.Alloc) / 1024 / 1024

	// Chrome memory via docker stats
	chromeMemMB := getChromeMemory(containerID)

	fmt.Println("┌─ Memory Usage ──────────────────────────────────────────────────────┐")
	fmt.Printf("│  Crema (per page)  %7.1f MB                                       │\n", cremaPageMB)
	fmt.Printf("│  Crema (total)     %7.1f MB                                       │\n", cremaTotalMB)
	fmt.Printf("│  Chrome (total)    %7.1f MB                                       │\n", chromeMemMB)
	if chromeMemMB > 0 {
		fmt.Printf("│  → Crema uses %.0fx less memory                                     │\n", chromeMemMB/cremaTotalMB)
	}
	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()
	browser.Close()

	// ── Navigation Benchmark ────────────────────────────
	fmt.Println("┌─ Navigation + Screenshot ───────────────────────────────────────────┐")
	fmt.Printf("│  %-36s  %7s  %7s  %5s │\n", "URL", "Nav", "Screen", "Links")
	fmt.Println("│  " + strings.Repeat("─", 63) + "  │")

	// Crema runs
	cremaResults := benchCrema()
	for _, r := range cremaResults {
		short := r.URL
		if len(short) > 36 { short = short[:36] }
		fmt.Printf("│  %-36s  %5.0fms  %5.0fms  %5d │  Crema\n",
			short, r.NavMs, r.ScreenMs, r.Links)
	}

	fmt.Println("│  " + strings.Repeat("─", 63) + "  │")

	// Chrome runs
	chromeResults := benchChrome(containerID)
	for _, r := range chromeResults {
		short := r.URL
		if len(short) > 36 { short = short[:36] }
		fmt.Printf("│  %-36s  %5.0fms  %5.0fms  %5d │  Chrome\n",
			short, r.NavMs, r.ScreenMs, r.Links)
	}

	fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
	fmt.Println()

	// ── Speed comparison ────────────────────────────────
	if len(cremaResults) > 0 && len(chromeResults) > 0 {
		fmt.Println("┌─ Speed Comparison (Navigate + Screenshot) ─────────────────────────┐")
		for i := range cremaResults {
			if i >= len(chromeResults) { break }
			cr := cremaResults[i]
			ch := chromeResults[i]
			crTotal := cr.NavMs + cr.ScreenMs
			chTotal := ch.NavMs + ch.ScreenMs
			ratio := ""
			if crTotal < chTotal {
				ratio = fmt.Sprintf("Crema %.1fx faster", chTotal/crTotal)
			} else if chTotal > 0 {
				ratio = fmt.Sprintf("Chrome %.1fx faster", crTotal/chTotal)
			}
			short := cr.URL
			if len(short) > 36 { short = short[:36] }
			fmt.Printf("│  %-36s  %s\n", short, ratio)
		}
		fmt.Println("└─────────────────────────────────────────────────────────────────────┘")
		fmt.Println()
	}

	// ── Summary ─────────────────────────────────────────
	fmt.Println("══════════════════════════════════════════════════════════════════════")
	fmt.Println("                            SUMMARY")
	fmt.Println("══════════════════════════════════════════════════════════════════════")
	fmt.Printf("  Image size:    Crema %5.1f MB  vs  Chrome %5.0f MB  (%.0fx smaller)\n",
		cremaDisk, chromeDisk, chromeDisk/cremaDisk)
	fmt.Printf("  Startup:       Crema %5.1f ms  vs  Chrome %5.0f ms\n",
		cremaStartMs, chromeStartMs)
	fmt.Printf("  Memory:        Crema %5.1f MB  vs  Chrome %5.0f MB  (%.0fx less)\n",
		cremaTotalMB, chromeMemMB, chromeMemMB/cremaTotalMB)
	fmt.Println("══════════════════════════════════════════════════════════════════════")
}

func benchCrema() []navResult {
	browser := crema.NewBrowser()
	defer browser.Close()

	var results []navResult
	for _, url := range urls {
		p := browser.NewPage()

		start := time.Now()
		err := p.Navigate(url)
		navMs := float64(time.Since(start).Milliseconds())
		if err != nil {
			results = append(results, navResult{URL: url, NavMs: navMs})
			continue
		}

		title := p.Title()
		links := len(p.QuerySelectorAll("a"))

		start = time.Now()
		p.Screenshot()
		screenMs := float64(time.Since(start).Milliseconds())

		results = append(results, navResult{
			URL: url, NavMs: navMs, ScreenMs: screenMs,
			Title: title, Links: links,
		})
	}
	return results
}

func benchChrome(containerID string) []navResult {
	// Get Chrome's debugger WebSocket URL
	wsURL := getChromeWSURL(containerID)
	if wsURL == "" {
		fmt.Println("  Could not get Chrome WS URL")
		return nil
	}

	// Run puppeteer benchmark script
	scriptPath, _ := filepath.Abs("cmd/benchmark/chrome_bench.js")
	cmd := exec.Command("node", scriptPath, wsURL)
	cmd.Dir = "/tmp" // so it finds node_modules
	out, err := cmd.Output()
	if err != nil {
		fmt.Printf("  Chrome bench error: %v\n", err)
		if exitErr, ok := err.(*exec.ExitError); ok {
			fmt.Printf("  stderr: %s\n", string(exitErr.Stderr))
		}
		return nil
	}

	// Parse JSON results
	var raw []struct {
		URL     string `json:"url"`
		NavTime int    `json:"navTime"`
		SSTime  int    `json:"ssTime"`
		Title   string `json:"title"`
		Links   int    `json:"links"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		fmt.Printf("  Chrome parse error: %v (output: %s)\n", err, string(out))
		return nil
	}

	var results []navResult
	for _, r := range raw {
		results = append(results, navResult{
			URL: r.URL, NavMs: float64(r.NavTime), ScreenMs: float64(r.SSTime),
			Title: r.Title, Links: r.Links,
		})
	}
	return results
}

func startChrome() string {
	// Kill any existing bench container
	exec.Command("docker", "rm", "-f", "bench-chrome").Run()

	cmd := exec.Command("docker", "run", "--rm", "-d",
		"--name", "bench-chrome",
		"-p", "9223:9222",
		"chromedp/headless-shell:latest",
		"--no-sandbox", "--disable-gpu", "--remote-debugging-address=0.0.0.0", "--remote-debugging-port=9222")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("  Failed to start Chrome: %v (%s)\n", err, string(out))
		return ""
	}
	containerID := strings.TrimSpace(string(out))

	// Wait for Chrome to be ready
	for i := 0; i < 100; i++ {
		time.Sleep(200 * time.Millisecond)
		check := exec.Command("curl", "-s", "http://localhost:9223/json/version")
		if o, e := check.Output(); e == nil && len(o) > 10 {
			return containerID
		}
	}
	return containerID
}

func getChromeWSURL(containerID string) string {
	cmd := exec.Command("curl", "-s", "http://localhost:9223/json/version")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	var version map[string]string
	json.Unmarshal(out, &version)
	ws := version["webSocketDebuggerUrl"]
	// Replace internal 0.0.0.0 with localhost
	ws = strings.Replace(ws, "0.0.0.0:9222", "localhost:9223", 1)
	return ws
}

func getChromeMemory(containerID string) float64 {
	cmd := exec.Command("docker", "stats", "--no-stream", "--format", "{{.MemUsage}}", containerID)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	// Format: "123.4MiB / 15.5GiB"
	parts := strings.Split(s, "/")
	if len(parts) == 0 {
		return 0
	}
	mem := strings.TrimSpace(parts[0])
	var val float64
	if strings.HasSuffix(mem, "GiB") {
		fmt.Sscanf(mem, "%fGiB", &val)
		val *= 1024
	} else if strings.HasSuffix(mem, "MiB") {
		fmt.Sscanf(mem, "%fMiB", &val)
	} else if strings.HasSuffix(mem, "KiB") {
		fmt.Sscanf(mem, "%fKiB", &val)
		val /= 1024
	}
	return val
}

func getDockerImageSize(name string) float64 {
	cmd := exec.Command("docker", "images", name, "--format", "{{.Size}}")
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return 0
	}
	var size float64
	if strings.HasSuffix(s, "GB") {
		fmt.Sscanf(s, "%fGB", &size)
		size *= 1024
	} else if strings.HasSuffix(s, "MB") {
		fmt.Sscanf(s, "%fMB", &size)
	}
	return size
}

func runCremaOnly() {
	fmt.Println("\n── Crema-only benchmark ───")
	results := benchCrema()
	for _, r := range results {
		fmt.Printf("  %-40s  nav: %5.0fms  screenshot: %5.0fms  links: %d\n",
			r.URL, r.NavMs, r.ScreenMs, r.Links)
	}
}

// suppress unused import
var _ = os.Stat
