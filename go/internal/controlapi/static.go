package controlapi

import (
	"embed"
	"io/fs"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

//go:embed all:web_dist
var embeddedWebFS embed.FS

// StaticHandler returns a Gin handler that serves the SPA bundle.
// Reads from the directory at TERMIX_CONTROL_WEB_DIR if set (dev mode),
// otherwise from the go:embed-ed web_dist subtree (prod mode).
//
// Routing:
//   - exact-file match (e.g. /assets/index-abc.js) → serves the file
//   - exact /index.html (or /) → serves index.html
//   - any other unknown path NOT under /assets → SPA fallback to index.html
//   - /assets/<missing> → 404 (don't fall back, lets the bundler error correctly)
func StaticHandler() gin.HandlerFunc {
	var fsys fs.FS
	if devDir := os.Getenv("TERMIX_CONTROL_WEB_DIR"); devDir != "" {
		fsys = os.DirFS(devDir)
	} else {
		sub, err := fs.Sub(embeddedWebFS, "web_dist")
		if err != nil {
			sub = embeddedWebFS // shouldn't happen, but degrade gracefully
		}
		fsys = sub
	}
	fileServer := http.FileServerFS(fsys)
	return func(c *gin.Context) {
		urlPath := c.Request.URL.Path
		rel := strings.TrimPrefix(urlPath, "/")
		if rel == "" {
			rel = "index.html"
		}
		// For non-/assets paths, fall back to index.html if the file doesn't exist.
		// /assets/* paths return 404 (correctness for missing bundle artifacts).
		if !strings.HasPrefix(urlPath, "/assets") {
			if _, err := fs.Stat(fsys, rel); err != nil {
				c.Request.URL.Path = "/"
			}
		}
		fileServer.ServeHTTP(c.Writer, c.Request)
	}
}
