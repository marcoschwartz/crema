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
	maxW := cremaImg.Bounds().Dx()
	if chromeImg.Bounds().Dx() < maxW { maxW = chromeImg.Bounds().Dx() }
	maxH := cremaImg.Bounds().Dy()
	if chromeImg.Bounds().Dy() < maxH { maxH = chromeImg.Bounds().Dy() }
	if maxW > w { maxW = w }
	if maxH > h { maxH = h }

	diff := image.NewRGBA(image.Rect(0, 0, maxW, maxH))

	// ── Block-based structural comparison ──
	// Compare blocks of 16x16 pixels by average color.
	// This ignores text rendering differences and focuses on layout:
	// - Background colors (right area has right bg?)
	// - Element positions (content in same vertical region?)
	// - Structural shapes (boxes, headers, footers)
	blockSize := 16
	blocksW := maxW / blockSize
	blocksH := maxH / blockSize
	totalBlocks := 0
	matchBlocks := 0

	regionSize := blocksH / 5
	if regionSize < 1 { regionSize = 1 }
	regionMatch := make([]int, 5)
	regionTotal := make([]int, 5)

	for by := 0; by < blocksH; by++ {
		region := by / regionSize
		if region >= 5 { region = 4 }

		for bx := 0; bx < blocksW; bx++ {
			totalBlocks++
			regionTotal[region]++

			// Average color of this block in both images
			var cr, cg, cb, hr, hg, hb float64
			count := 0
			for dy := 0; dy < blockSize; dy++ {
				for dx := 0; dx < blockSize; dx++ {
					px := bx*blockSize + dx
					py := by*blockSize + dy
					if px >= maxW || py >= maxH { continue }
					count++

					r1, g1, b1, _ := cremaImg.At(px, py).RGBA()
					r2, g2, b2, _ := chromeImg.At(px, py).RGBA()
					cr += float64(r1 >> 8); cg += float64(g1 >> 8); cb += float64(b1 >> 8)
					hr += float64(r2 >> 8); hg += float64(g2 >> 8); hb += float64(b2 >> 8)
				}
			}
			if count == 0 { continue }
			fc := float64(count)
			cr /= fc; cg /= fc; cb /= fc
			hr /= fc; hg /= fc; hb /= fc

			// ── Layout-focused comparison ──
			// Classify each block as: empty (background), content (text/images), or UI (buttons/borders)
			// Match if both blocks have the same classification, regardless of color.
			cremaLum := cr*0.299 + cg*0.587 + cb*0.114
			chromeLum := hr*0.299 + hg*0.587 + hb*0.114

			// Variance within block — high variance means text/edges, low means flat bg
			var cremaVar, chromeVar float64
			for dy := 0; dy < blockSize; dy++ {
				for dx := 0; dx < blockSize; dx++ {
					px := bx*blockSize + dx
					py := by*blockSize + dy
					if px >= maxW || py >= maxH { continue }
					r1, g1, b1, _ := cremaImg.At(px, py).RGBA()
					l1 := float64(r1>>8)*0.299 + float64(g1>>8)*0.587 + float64(b1>>8)*0.114
					cremaVar += (l1 - cremaLum) * (l1 - cremaLum)
					r2, g2, b2, _ := chromeImg.At(px, py).RGBA()
					l2 := float64(r2>>8)*0.299 + float64(g2>>8)*0.587 + float64(b2>>8)*0.114
					chromeVar += (l2 - chromeLum) * (l2 - chromeLum)
				}
			}
			cremaVar /= fc
			chromeVar /= fc

			// Block type: "empty" (low variance, any brightness) vs "content" (high variance = text/edges)
			cremaHasContent := cremaVar > 200   // text/images have high pixel variance
			chromeHasContent := chromeVar > 200

			// Match conditions (layout-focused, color-independent):
			// 1. Both empty (both are background — color doesn't matter)
			// 2. Both have content (both have text/images — position matches)
			// 3. Similar brightness class (both dark or both light)
			bothEmpty := !cremaHasContent && !chromeHasContent
			bothContent := cremaHasContent && chromeHasContent
			sameBrightness := math.Abs(cremaLum - chromeLum) < 80

			matched := bothEmpty || bothContent || sameBrightness

			// Paint diff block
			for dy := 0; dy < blockSize; dy++ {
				for dx := 0; dx < blockSize; dx++ {
					px := bx*blockSize + dx
					py := by*blockSize + dy
					if px >= maxW || py >= maxH { continue }
					if matched {
						// Show crema image with slight green tint for matched
						r1, g1, b1, _ := cremaImg.At(px, py).RGBA()
						diff.Set(px, py, color.RGBA{uint8(r1>>8), uint8(g1>>8), uint8(b1>>8), 255})
					} else {
						// Red for mismatched layout
						diff.Set(px, py, color.RGBA{200, 30, 30, 255})
					}
				}
			}

			if matched {
				matchBlocks++
				regionMatch[region]++
			}
		}
	}

	score := 0.0
	if totalBlocks > 0 {
		score = float64(matchBlocks) / float64(totalBlocks)
	}

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
