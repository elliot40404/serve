package main

import (
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	portFlag := flag.String("port", "8080", "Port to listen on")
	dirFlag := flag.String("dir", "", "Directory to serve (default: current directory)")
	passwordCmdFlag := flag.String("password", "", "Password to protect UI (takes precedence over SERVE_PASS env var)")
	passFlag := flag.String("pass", "", "Alias for --password")
	flag.Parse()

	rootDir := *dirFlag
	if rootDir == "" {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			log.Printf("Error getting current directory: %v", err)
			return
		}
	}
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		log.Printf("Directory does not exist: %s", rootDir)
		return
	} else if err != nil {
		log.Printf("Error stating directory %s: %v", rootDir, err)
		return
	}
	log.Printf("Serving directory: %s", rootDir)

	var effectivePassword string
	switch {
	case *passwordCmdFlag != "":
		effectivePassword = *passwordCmdFlag
	case *passFlag != "":
		effectivePassword = *passFlag
	default:
		effectivePassword = os.Getenv("SERVE_PASS")
	}

	appServer, err := NewServer(rootDir, effectivePassword)
	if err != nil {
		log.Printf("Error creating server: %v", err)
		return
	}
	defer func() {
		if err := appServer.watcher.Close(); err != nil {
			log.Printf("Error closing server watcher: %v", err)
		}
	}()

	mux := http.NewServeMux()

	// Static file handler for UI assets (CSS, JS) from embedded filesystem.
	// This itself will be wrapped by authMiddleware for most paths.
	staticContentFS, err := fs.Sub(staticFilesystem, "static")
	if err != nil {
		log.Printf("Failed to create sub static content FS: %v", err)
		return
	}
	staticHandler := http.StripPrefix("/static/", http.FileServer(http.FS(staticContentFS)))
	mux.Handle("/static/", authMiddleware(appServer, staticHandler.ServeHTTP))

	// Login and Logout handlers are NOT wrapped by the main authMiddleware directly here,
	// as they need to be accessible to unauthenticated users.
	// handleLoginPost itself checks s.authEnabled internally.
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleLoginPost(appServer, w, r)
		} else {
			handleLoginGet(appServer, w, r)
		}
	})
	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		handleLogout(appServer, w, r)
	})

	// Protected handlers
	mux.HandleFunc("/", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleIndex(appServer, w, r)
	}))
	mux.HandleFunc("/browse/", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleBrowse(appServer, w, r)
	}))
	mux.HandleFunc("/api/files", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleAPI(appServer, w, r)
	}))
	mux.HandleFunc("/api/random-media", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleRandomMedia(appServer, w, r)
	}))
	mux.HandleFunc("/ws", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleWebSocket(appServer, w, r)
	}))
	mux.HandleFunc("/files/", authMiddleware(appServer, func(w http.ResponseWriter, r *http.Request) {
		handleFiles(appServer, w, r)
	}))

	loggedMux := logRequest(mux)

	log.Printf("Starting server on port %s", *portFlag)
	log.Printf("Access the server at: http://localhost:%s", *portFlag)
	log.Printf("LAN access might be available at: http://<your-local-ip>:%s", *portFlag)

	srv := &http.Server{
		Addr:         "0.0.0.0:" + *portFlag,
		Handler:      loggedMux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	err = srv.ListenAndServe()
	if err != nil {
		log.Printf("Server failed to start: %v", err)
		return
	}
}
