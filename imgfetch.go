package crema

import (
	"image"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

// imageCache caches fetched images to avoid re-downloading.
var (
	imageCache   = map[string]*image.RGBA{}
	imageCacheMu sync.Mutex
)

// fetchImage downloads an image URL and returns it as RGBA.
// Returns nil on error. Results are cached.
func fetchImage(imgURL string, pageURL string, client *http.Client) *image.RGBA {
	// Resolve relative URLs
	if strings.HasPrefix(imgURL, "//") {
		imgURL = "https:" + imgURL
	} else if strings.HasPrefix(imgURL, "/") && pageURL != "" {
		imgURL = extractOrigin(pageURL) + imgURL
	}
	if !strings.HasPrefix(imgURL, "http") {
		return nil
	}

	// Check cache
	imageCacheMu.Lock()
	if cached, ok := imageCache[imgURL]; ok {
		imageCacheMu.Unlock()
		return cached
	}
	imageCacheMu.Unlock()

	// Skip data URIs (too complex to decode here)
	if strings.HasPrefix(imgURL, "data:") {
		return nil
	}

	// Fetch with timeout
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	req, err := http.NewRequest("GET", imgURL, nil)
	if err != nil { return nil }
	req.Header.Set("Accept", "image/*")

	resp, err := client.Do(req)
	if err != nil { return nil }
	defer resp.Body.Close()

	if resp.StatusCode != 200 { return nil }

	// Limit size to 5MB
	limitedBody := io.LimitReader(resp.Body, 5*1024*1024)

	// Decode
	img, _, err := image.Decode(limitedBody)
	if err != nil { return nil }

	// Convert to RGBA
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	// Cache
	imageCacheMu.Lock()
	imageCache[imgURL] = rgba
	imageCacheMu.Unlock()

	return rgba
}

// activePageURL and activeClient are set during layout for image fetching.
var (
	activePageURL string
	activeClient  *http.Client
)
