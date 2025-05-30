package main

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

func authMiddleware(s *Server, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authEnabled {
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && s.isValidSession(cookie.Value) {
			next.ServeHTTP(w, r)
			return
		}

		// Allow access to login page and its potential specific assets if any
		// For this setup, login page is self-contained or uses minimal styling.
		// If login page had /static/login.css, you'd add an exception here.
		if r.URL.Path == "/login" {
			next.ServeHTTP(w, r) // This allows login GET/POST through if not caught by specific routes first
			return
		}

		// For API requests, return 401
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ws") {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		// For other requests (HTML pages, /static/ for main UI), redirect to login
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

func handleLoginGet(s *Server, w http.ResponseWriter, r *http.Request) {
	if s.authEnabled {
		cookie, err := r.Cookie(sessionCookieName)
		if err == nil && s.isValidSession(cookie.Value) {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
	}
	// Pass nil data for now, can add {Error: "message"} later
	err := s.loginTemplate.Execute(w, nil)
	if err != nil {
		log.Printf("Login template error: %v", err)
		http.Error(w, "Error rendering login page", http.StatusInternalServerError)
	}
}

func handleLoginPost(s *Server, w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled { // Should not happen if routes are set up correctly
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	password := r.FormValue("password")
	err := bcrypt.CompareHashAndPassword(s.hashedPassword, []byte(password))
	if err == nil { // Password matches
		token, tokenErr := s.generateSessionToken()
		if tokenErr != nil {
			log.Printf("Error generating session token: %v", tokenErr)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		s.addSession(token)
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			// Secure: true, // Uncomment if serving over HTTPS
		})
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Password does not match
	log.Println("Failed login attempt")
	loginData := map[string]string{"Error": "Invalid password."}
	w.WriteHeader(http.StatusUnauthorized) // Keep on login page but indicate error
	if err := s.loginTemplate.Execute(w, loginData); err != nil {
		log.Printf("Login template error after failed attempt: %v", err)
		// Fallback if template fails
		http.Error(w, "Invalid password and error rendering page.", http.StatusInternalServerError)
	}
}

func handleLogout(s *Server, w http.ResponseWriter, r *http.Request) {
	if !s.authEnabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		s.removeSession(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1, // Expire cookie
		SameSite: http.SameSiteLaxMode,
		// Secure: true, // Uncomment if serving over HTTPS
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

// Existing handlers (handleIndex, handleBrowse, handleAPI, etc.)
// These will be wrapped by the authMiddleware in main.go
// No changes needed to their internal logic for this feature

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[%s] %s %s - %s",
			start.Format("2006-01-02 15:04:05"),
			r.Method,
			r.URL.Path,
			r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func handleIndex(s *Server, w http.ResponseWriter, _ *http.Request) {
	data := map[string]any{
		"RandomMediaEnabled": s.randomBtn,
	}
	err := s.template.Execute(w, data)
	if err != nil {
		log.Printf("Template error on /: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
	}
}

func handleBrowse(s *Server, w http.ResponseWriter, _ *http.Request) {
	err := s.template.Execute(w, nil)
	if err != nil {
		log.Printf("Template error on /browse/: %v", err)
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
	}
}

func handleAPI(s *Server, w http.ResponseWriter, r *http.Request) {
	relativePath := r.URL.Query().Get("path")
	data, err := getDirectoryListing(s.rootDir, relativePath)
	if err != nil {
		log.Printf("Error getting directory listing for API path '%s': %v", relativePath, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sortBy := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	if sortBy != "" {
		sortFiles(data.Files, sortBy, order)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error encoding API response for path '%s': %v", relativePath, err)
	}
}

func handleRandomMedia(s *Server, w http.ResponseWriter, r *http.Request) {
	relativePath := r.URL.Query().Get("path")
	mediaFile, err := getRandomMediaFile(s.rootDir, relativePath)
	if err != nil {
		log.Printf("Error getting random media for path '%s': %v", relativePath, err)
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, err = w.Write([]byte(mediaFile))
	if err != nil {
		log.Printf("Error writing response for path '%s': %v", relativePath, err)
	}
}

func handleWebSocket(s *Server, w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()
	s.clients[conn] = true
	log.Printf("Client %s connected via WebSocket", conn.RemoteAddr())
	defer func() {
		delete(s.clients, conn)
		log.Printf("Client %s disconnected from WebSocket", conn.RemoteAddr())
	}()
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Unexpected WebSocket close error for client %s: %v", conn.RemoteAddr(), err)
			} else {
				log.Printf("WebSocket connection closed for client %s", conn.RemoteAddr())
			}
			break
		}
	}
}

func handleFiles(s *Server, w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/files/")
	unescapedPath, err := url.PathUnescape(relativePath)
	if err != nil {
		log.Printf("Error unescaping path '%s': %v", relativePath, err)
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	path := filepath.Clean(unescapedPath)
	if strings.Contains(path, "..") {
		http.Error(w, "Forbidden: path attempts to traverse up", http.StatusForbidden)
		return
	}
	fullPath := filepath.Join(s.rootDir, path)
	absRoot, _ := filepath.Abs(s.rootDir)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "Forbidden: path outside root directory", http.StatusForbidden)
		return
	}
	http.ServeFile(w, r, fullPath)
}
