package main

import (
	"bytes"
	"html/template"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/alvinchoong/go-httphandler"
)

// TemplateData holds data for rendering the index template
type TemplateData struct {
	PrefixPath string
	CSSHash    string
	JSHash     string
}

// indexHandler serves the main HTML page with embedded template
func indexHandler(config Config, assets map[string]AssetInfo) httphandler.RequestHandler {
	return func(r *http.Request) httphandler.Responder {
		// Only serve on exact path match
		if r.URL.Path != "/" && r.URL.Path != config.PrefixPath+"/" {
			return errorResponder(http.StatusNotFound, "Not found")
		}

		// Get asset info
		cssInfo, cssOk := assets["css/styles.css"]
		jsInfo, jsOk := assets["js/app.js"]

		if !cssOk || !jsOk {
			return errorResponder(http.StatusInternalServerError, "Assets not found")
		}

		// Parse and execute template
		tmpl, err := template.New("index").Parse(indexTemplate)
		if err != nil {
			return errorResponder(http.StatusInternalServerError, "Template parse error: "+err.Error())
		}

		data := TemplateData{
			PrefixPath: config.PrefixPath,
			CSSHash:    cssInfo.Hash,
			JSHash:     jsInfo.Hash,
		}

		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			return errorResponder(http.StatusInternalServerError, "Template execution error: "+err.Error())
		}

		return &htmlResponder{content: buf.Bytes()}
	}
}

// staticHandler serves embedded static files with proper content types
func staticHandler(config Config, assets map[string]AssetInfo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Remove prefix path from URL
		urlPath := strings.TrimPrefix(r.URL.Path, config.PrefixPath)
		urlPath = strings.TrimPrefix(urlPath, "/")

		// Try to find the original file by checking against hashed versions
		var originalPath string
		for orig, info := range assets {
			if urlPath == info.Path {
				originalPath = orig
				break
			}
		}

		if originalPath == "" {
			http.NotFound(w, r)
			return
		}

		// Get file content
		content, err := getFileContent(originalPath)
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}

		// Set content type based on file extension
		ext := path.Ext(originalPath)
		switch ext {
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		}

		// Set cache headers for hashed assets
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

		// Write content
		w.Write(content)
	}
}

// htmlResponder implements httphandler.Responder for HTML responses
type htmlResponder struct {
	content []byte
}

func (h *htmlResponder) Respond(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(h.content)
}

// errorResponder creates a responder for error responses
func errorResponder(statusCode int, message string) httphandler.Responder {
	return httphandler.ResponderFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		io.WriteString(w, message)
	})
}
