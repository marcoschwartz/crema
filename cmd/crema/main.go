package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/marcoschwartz/crema"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Crema — lightweight headless browser")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  crema <url> [--screenshot <file.png>] [--proxy <url>]")
		fmt.Fprintln(os.Stderr, "  crema --cdp [--port 9222] [--proxy <url>]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  crema https://example.com")
		fmt.Fprintln(os.Stderr, "  crema https://example.com --screenshot page.png")
		fmt.Fprintln(os.Stderr, "  crema --cdp --port 9222")
		fmt.Fprintln(os.Stderr, "  crema --cdp --proxy http://user:pass@proxy:8080")
		os.Exit(1)
	}

	// Parse flags
	cdpMode := false
	port := 9222
	proxyURL := ""
	screenshotPath := ""
	targetURL := ""

	for i := 1; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--cdp":
			cdpMode = true
		case "--port":
			if i+1 < len(os.Args) {
				i++
				port, _ = strconv.Atoi(os.Args[i])
			}
		case "--proxy":
			if i+1 < len(os.Args) {
				i++
				proxyURL = os.Args[i]
			}
		case "--screenshot":
			if i+1 < len(os.Args) {
				i++
				screenshotPath = os.Args[i]
			}
		default:
			if targetURL == "" && !cdpMode {
				targetURL = os.Args[i]
			}
		}
	}

	// Create browser
	var browser *crema.Browser
	if proxyURL != "" {
		browser = crema.NewBrowserWithProxy(proxyURL)
	} else {
		browser = crema.NewBrowser()
	}
	defer browser.Close()

	if cdpMode {
		// Start CDP server
		server := crema.NewCDPServer(browser, port)
		fmt.Printf("Crema CDP server on ws://localhost:%d\n", port)
		fmt.Printf("Connect with: puppeteer.connect({ browserWSEndpoint: 'ws://localhost:%d/devtools/browser' })\n", port)
		if err := server.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "CDP server error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Single URL mode
		if targetURL == "" {
			fmt.Fprintln(os.Stderr, "Error: URL required")
			os.Exit(1)
		}

		page := browser.NewPage()
		err := page.Navigate(targetURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Title: %s\n", page.Title())
		fmt.Printf("URL: %s\n", targetURL)

		if screenshotPath != "" {
			err = page.ScreenshotFile(screenshotPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Screenshot error: %v\n", err)
				os.Exit(1)
			}
			info, _ := os.Stat(screenshotPath)
			fmt.Printf("Screenshot: %s (%d bytes)\n", screenshotPath, info.Size())
		}
	}
}
