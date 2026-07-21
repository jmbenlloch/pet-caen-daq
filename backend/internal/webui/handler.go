// Package webui serves the built operator application without weakening the
// same-origin boundary around the ConnectRPC API.
package webui

import (
	"bytes"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// New validates a built frontend directory and returns its HTTP handler.
func New(directory string) (http.Handler, error) {
	if directory == "" {
		return nil, fmt.Errorf("frontend directory is empty")
	}
	root := os.DirFS(directory)
	if err := validate(root); err != nil {
		return nil, fmt.Errorf("frontend directory %q: %w", directory, err)
	}
	return newHandler(root), nil
}

func validate(root fs.FS) error {
	info, err := fs.Stat(root, "index.html")
	if err != nil {
		return fmt.Errorf("read index.html: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("index.html is not a regular file")
	}
	return fs.WalkDir(root, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symbolic link %q is not allowed", name)
		}
		return nil
	})
}

type handler struct {
	root  fs.FS
	files http.Handler
}

func newHandler(root fs.FS) http.Handler {
	return &handler{root: root, files: http.FileServer(http.FS(root))}
}

func (h *handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		response.Header().Set("Allow", "GET, HEAD")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+request.URL.Path), "/")
	if name == "." || name == "" {
		h.serveIndex(response, request)
		return
	}
	if info, err := fs.Stat(h.root, name); err == nil && info.Mode().IsRegular() {
		if strings.HasPrefix(name, "assets/") {
			response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			response.Header().Set("Cache-Control", "no-cache")
		}
		h.files.ServeHTTP(response, request)
		return
	}

	// Browser routes have no extension and receive the application shell.
	// Missing assets and other file-like requests remain honest 404s.
	if path.Ext(name) == "" && !strings.HasPrefix(name, "assets/") {
		h.serveIndex(response, request)
		return
	}
	http.NotFound(response, request)
}

func (h *handler) serveIndex(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-cache")
	content, err := fs.ReadFile(h.root, "index.html")
	if err != nil {
		http.Error(response, "operator application unavailable", http.StatusInternalServerError)
		return
	}
	info, err := fs.Stat(h.root, "index.html")
	if err != nil {
		http.Error(response, "operator application unavailable", http.StatusInternalServerError)
		return
	}
	http.ServeContent(response, request, "index.html", info.ModTime(), bytes.NewReader(content))
}
