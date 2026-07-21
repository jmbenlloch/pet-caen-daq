package webui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandlerServesShellAssetsAndBrowserRoutes(t *testing.T) {
	handler := newHandler(fstest.MapFS{
		"index.html":            {Data: []byte("<main>operator</main>")},
		"assets/application.js": {Data: []byte("console.log('daq')")},
	})
	tests := []struct {
		name, target, body, cache string
		status                    int
	}{
		{name: "root", target: "/", body: "operator", cache: "no-cache", status: http.StatusOK},
		{name: "browser route", target: "/runs/latest", body: "operator", cache: "no-cache", status: http.StatusOK},
		{name: "asset", target: "/assets/application.js", body: "console.log", cache: "public, max-age=31536000, immutable", status: http.StatusOK},
		{name: "missing asset", target: "/assets/missing.js", body: "404 page not found", status: http.StatusNotFound},
		{name: "missing file", target: "/missing.txt", body: "404 page not found", status: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.target, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status || !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("status=%d body=%q", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Cache-Control"); got != test.cache {
				t.Fatalf("Cache-Control=%q want %q", got, test.cache)
			}
		})
	}
}

func TestHandlerRejectsStateChangingMethods(t *testing.T) {
	handler := newHandler(fstest.MapFS{"index.html": {Data: []byte("operator")}})
	request := httptest.NewRequest(http.MethodPost, "/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("status=%d Allow=%q", response.Code, response.Header().Get("Allow"))
	}
}

func TestNewValidatesIndexAndRejectsSymlinks(t *testing.T) {
	missing := t.TempDir()
	if _, err := New(missing); err == nil || !strings.Contains(err.Error(), "index.html") {
		t.Fatalf("missing index error=%v", err)
	}

	root := t.TempDir()
	if err := os.WriteFile(root+"/index.html", []byte("operator"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"/index.html", root+"/linked.html"); err != nil {
		if os.IsPermission(err) {
			t.Skip("symlinks unavailable")
		}
		t.Fatal(err)
	}
	if _, err := New(root); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink error=%v", err)
	}
}

func TestValidateRejectsNonRegularIndex(t *testing.T) {
	err := validate(fstest.MapFS{"index.html": &fstest.MapFile{Mode: fs.ModeDir}})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error=%v", err)
	}
}
