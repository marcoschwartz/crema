package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	files := map[string]string{
		"/login.png":   "/tmp/crema_login_page.png",
		"/test.png":    "/tmp/crema_test.png",
		"/example.png": "/tmp/crema_example.png",
		"/flex.png":    "/tmp/crema_flex.png",
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if f, ok := files[r.URL.Path]; ok {
			data, _ := os.ReadFile(f)
			w.Header().Set("Content-Type", "image/png")
			w.Write(data)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, "<h1>Crema Screenshots</h1>")
		for k := range files {
			fmt.Fprintf(w, "<p><a href='%s'>%s</a></p>", k, k)
		}
	})
	fmt.Println("Serving on :9876")
	http.ListenAndServe("0.0.0.0:9876", nil)
}
