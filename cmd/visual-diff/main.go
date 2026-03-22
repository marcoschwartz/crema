package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/marcoschwartz/crema"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: visual-diff <url> [--output-dir <dir>]")
		fmt.Println("Compares Crema screenshot vs Chrome headless screenshot")
		os.Exit(1)
	}

	url := os.Args[1]
	outDir := "/tmp/visual-diff"
	for i := 2; i < len(os.Args); i++ {
		if os.Args[i] == "--output-dir" && i+1 < len(os.Args) {
			i++
			outDir = os.Args[i]
		}
	}
	os.MkdirAll(outDir, 0755)

	width := 1280
	height := 900

	fmt.Printf("URL: %s\n", url)
	fmt.Printf("Viewport: %dx%d\n\n", width, height)

	// 1. Crema screenshot
	fmt.Print("Crema screenshot... ")
	start := time.Now()
	cremaImg := takeCremaScreenshot(url, width, height)
	cremaMs := time.Since(start).Milliseconds()
	cremaPath := outDir + "/crema.png"
	saveImage(cremaImg, cremaPath)
	fmt.Printf("%dms (%dx%d)\n", cremaMs, cremaImg.Bounds().Dx(), cremaImg.Bounds().Dy())

	// 2. Chrome screenshot
	fmt.Print("Chrome screenshot... ")
	start = time.Now()
	chromePath := outDir + "/chrome.png"
	chromeErr := takeChromeScreenshot(url, width, height, chromePath)
	chromeMs := time.Since(start).Milliseconds()
	if chromeErr != nil {
		fmt.Printf("ERROR: %v\n", chromeErr)
		fmt.Println("\nSkipping comparison — Chrome not available")
		return
	}
	chromeImg := loadImage(chromePath)
	if chromeImg == nil {
		fmt.Println("ERROR: could not load Chrome screenshot")
		return
	}
	fmt.Printf("%dms (%dx%d)\n", chromeMs, chromeImg.Bounds().Dx(), chromeImg.Bounds().Dy())

	// 3. Compare
	fmt.Print("\nComparing... ")
	diffImg, score, regionScores := compareImages(cremaImg, chromeImg, width, height)
	diffPath := outDir + "/diff.png"
	saveImage(diffImg, diffPath)
	fmt.Printf("done\n\n")

	// 4. Report
	fmt.Println("═══════════════════════════════════════════")
	fmt.Println("          VISUAL DIFF REPORT")
	fmt.Println("═══════════════════════════════════════════")
	fmt.Printf("  Overall similarity:  %.1f%%\n", score*100)
	fmt.Printf("  Crema time:          %dms\n", cremaMs)
	fmt.Printf("  Chrome time:         %dms\n", chromeMs)
	fmt.Println()
	fmt.Println("  Region scores (top→bottom):")
	for i, rs := range regionScores {
		bar := strings.Repeat("█", int(rs*20))
		gap := strings.Repeat("░", 20-int(rs*20))
		fmt.Printf("    %d. %s%s %.0f%%\n", i+1, bar, gap, rs*100)
	}
	fmt.Println()
	fmt.Printf("  Crema:  %s\n", cremaPath)
	fmt.Printf("  Chrome: %s\n", chromePath)
	fmt.Printf("  Diff:   %s\n", diffPath)
	fmt.Println("═══════════════════════════════════════════")
}

func takeCremaScreenshot(url string, w, h int) *image.RGBA {
	b := crema.NewBrowser()
	defer b.Close()
	p := b.NewPage()
	p.Navigate(url)

	data, _ := p.ScreenshotSize(w, h)
	if data == nil {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}

	f, _ := os.CreateTemp("", "crema-*.png")
	f.Write(data)
	f.Close()
	defer os.Remove(f.Name())

	img := loadImage(f.Name())
	if img == nil {
		return image.NewRGBA(image.Rect(0, 0, w, h))
	}
	return img
}

func takeChromeScreenshot(url string, w, h int, outPath string) error {
	// Start Chrome if not running
	containerName := "visual-diff-chrome"
	exec.Command("docker", "rm", "-f", containerName).Run()

	cmd := exec.Command("docker", "run", "--rm", "-d",
		"--name", containerName,
		"--network", "host",
		"chromedp/headless-shell:latest",
		"--no-sandbox", "--disable-gpu",
		"--remote-debugging-port=9224")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start chrome: %v (%s)", err, string(out))
	}
	defer exec.Command("docker", "rm", "-f", containerName).Run()

	// Wait for Chrome
	for i := 0; i < 100; i++ {
		time.Sleep(200 * time.Millisecond)
		check := exec.Command("curl", "-s", "http://localhost:9224/json/version")
		if out, err := check.Output(); err == nil && len(out) > 10 {
			break
		}
	}

	// Get WS URL
	wsCmd := exec.Command("curl", "-s", "http://localhost:9224/json/version")
	wsOut, err := wsCmd.Output()
	if err != nil {
		return fmt.Errorf("get ws url: %v", err)
	}
	var version map[string]string
	json.Unmarshal(wsOut, &version)
	wsURL := version["webSocketDebuggerUrl"]

	// Use node+puppeteer to take screenshot
	script := fmt.Sprintf(`
const puppeteer = require('puppeteer-core');
(async () => {
    const browser = await puppeteer.connect({ browserWSEndpoint: '%s' });
    const page = await browser.newPage();
    await page.setViewport({ width: %d, height: %d });
    await page.goto('%s', { waitUntil: 'networkidle2', timeout: 30000 });
    await page.screenshot({ path: '%s', fullPage: false });
    await browser.disconnect();
})();
`, wsURL, w, h, url, outPath)

	scriptPath := "/tmp/chrome-ss.cjs"
	os.WriteFile(scriptPath, []byte(script), 0644)
	defer os.Remove(scriptPath)

	nodeCmd := exec.Command("node", scriptPath)
	nodeCmd.Dir = "/tmp" // where puppeteer-core is installed
	if out, err := nodeCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("puppeteer: %v (%s)", err, string(out))
	}

	return nil
}

func compareImages(cremaImg, chromeImg *image.RGBA, w, h int) (*image.RGBA, float64, []float64) {
	// Normalize sizes
	maxW := cremaImg.Bounds().Dx()
	if chromeImg.Bounds().Dx() < maxW {
		maxW = chromeImg.Bounds().Dx()
	}
	maxH := cremaImg.Bounds().Dy()
	if chromeImg.Bounds().Dy() < maxH {
		maxH = chromeImg.Bounds().Dy()
	}
	if maxW > w { maxW = w }
	if maxH > h { maxH = h }

	diff := image.NewRGBA(image.Rect(0, 0, maxW, maxH))
	totalPixels := 0
	matchPixels := 0

	// Divide into 5 horizontal regions for region scoring
	regionSize := maxH / 5
	if regionSize < 1 { regionSize = 1 }
	regionMatch := make([]int, 5)
	regionTotal := make([]int, 5)

	for y := 0; y < maxH; y++ {
		region := y / regionSize
		if region >= 5 { region = 4 }

		for x := 0; x < maxW; x++ {
			totalPixels++
			regionTotal[region]++

			cr, cg, cb, _ := cremaImg.At(x, y).RGBA()
			hr, hg, hb, _ := chromeImg.At(x, y).RGBA()

			// Convert to 8-bit
			cr8, cg8, cb8 := cr>>8, cg>>8, cb>>8
			hr8, hg8, hb8 := hr>>8, hg>>8, hb>>8

			// Color distance
			dr := math.Abs(float64(cr8) - float64(hr8))
			dg := math.Abs(float64(cg8) - float64(hg8))
			db := math.Abs(float64(cb8) - float64(hb8))
			dist := (dr + dg + db) / 3

			if dist < 30 {
				// Similar enough — show original
				matchPixels++
				regionMatch[region]++
				diff.Set(x, y, cremaImg.At(x, y))
			} else {
				// Different — highlight in red
				intensity := uint8(math.Min(255, dist*2))
				diff.Set(x, y, color.RGBA{intensity, 0, 0, 255})
			}
		}
	}

	score := float64(matchPixels) / float64(totalPixels)

	regionScores := make([]float64, 5)
	for i := 0; i < 5; i++ {
		if regionTotal[i] > 0 {
			regionScores[i] = float64(regionMatch[i]) / float64(regionTotal[i])
		}
	}

	return diff, score, regionScores
}

func loadImage(path string) *image.RGBA {
	f, err := os.Open(path)
	if err != nil { return nil }
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil { return nil }

	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			rgba.Set(x, y, img.At(x, y))
		}
	}
	return rgba
}

func saveImage(img *image.RGBA, path string) {
	f, _ := os.Create(path)
	defer f.Close()
	enc := &png.Encoder{CompressionLevel: png.BestSpeed}
	enc.Encode(f, img)
}
