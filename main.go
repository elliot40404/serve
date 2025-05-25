package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
)

//go:embed templates/index.html
var templateFS embed.FS

//go:embed all:static
var staticFilesystem embed.FS

type FileInfo struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	Mode    string    `json:"mode"`
	ModTime time.Time `json:"modTime"`
	IsDir   bool      `json:"isDir"`
	Path    string    `json:"path"`
}

type DirectoryData struct {
	Files       []FileInfo `json:"files"`
	CurrentPath string     `json:"currentPath"`
	ParentPath  string     `json:"parentPath"`
	HasParent   bool       `json:"hasParent"`
}

type Server struct {
	rootDir   string
	upgrader  websocket.Upgrader
	clients   map[*websocket.Conn]bool
	watcher   *fsnotify.Watcher
	broadcast chan []byte
	template  *template.Template
}

func NewServer(rootDir string) (*Server, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	tmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded template: %v", err)
	}

	server := &Server{
		rootDir: rootDir,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
		clients:   make(map[*websocket.Conn]bool),
		watcher:   watcher,
		broadcast: make(chan []byte),
		template:  tmpl,
	}

	err = server.watchDirectory()
	if err != nil {
		return nil, err
	}
	go server.handleBroadcast()
	return server, nil
}

func (s *Server) watchDirectory() error {
	err := s.watcher.Add(s.rootDir)
	if err != nil {
		return err
	}
	filepath.WalkDir(s.rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			s.watcher.Add(path)
		}
		return nil
	})
	go func() {
		for {
			select {
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create == fsnotify.Create {
					if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
						s.watcher.Add(event.Name)
					}
				}
				data, _ := json.Marshal(map[string]any{"type": "update"})
				s.broadcast <- data
			case err, ok := <-s.watcher.Errors:
				if !ok {
					return
				}
				log.Println("Watcher error:", err)
			}
		}
	}()
	return nil
}

func (s *Server) handleBroadcast() {
	for {
		data := <-s.broadcast
		for client := range s.clients {
			err := client.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				client.Close()
				delete(s.clients, client)
			}
		}
	}
}

func (s *Server) getDirectoryListing(relativePath string) (*DirectoryData, error) {
	cleanPath := filepath.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}
	if strings.Contains(cleanPath, "..") {
		return nil, errors.New("invalid path")
	}
	fullPath := filepath.Join(s.rootDir, cleanPath)
	absRoot, _ := filepath.Abs(s.rootDir)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		return nil, errors.New("path outside root directory")
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}
	var files []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		var filePath string
		if cleanPath == "" {
			filePath = "/browse/" + url.PathEscape(info.Name())
		} else {
			filePath = "/browse/" + url.PathEscape(cleanPath) + "/" + url.PathEscape(info.Name())
		}
		if !info.IsDir() {
			if cleanPath == "" {
				filePath = "/files/" + url.PathEscape(info.Name())
			} else {
				filePath = "/files/" + url.PathEscape(cleanPath) + "/" + url.PathEscape(info.Name())
			}
		}
		files = append(files, FileInfo{
			Name:    info.Name(),
			Size:    info.Size(),
			Mode:    info.Mode().String(),
			ModTime: info.ModTime(),
			IsDir:   info.IsDir(),
			Path:    filePath,
		})
	}
	var parentPath string
	var hasParent bool
	if cleanPath != "" {
		parentDir := filepath.Dir(cleanPath)
		if parentDir == "." || parentDir == "/" {
			parentPath = "/browse/"
		} else {
			parentPath = "/browse/" + url.PathEscape(parentDir)
		}
		hasParent = true
	}
	return &DirectoryData{
		Files:       files,
		CurrentPath: cleanPath,
		ParentPath:  parentPath,
		HasParent:   hasParent,
	}, nil
}

func (*Server) sortFiles(files []FileInfo, sortBy, order string) {
	switch sortBy {
	case "name":
		sort.Slice(files, func(i, j int) bool {
			if order == "desc" {
				return files[i].Name > files[j].Name
			}
			return files[i].Name < files[j].Name
		})
	case "size":
		sort.Slice(files, func(i, j int) bool {
			if order == "desc" {
				return files[i].Size > files[j].Size
			}
			return files[i].Size < files[j].Size
		})
	case "date":
		sort.Slice(files, func(i, j int) bool {
			if order == "desc" {
				return files[i].ModTime.After(files[j].ModTime)
			}
			return files[i].ModTime.Before(files[j].ModTime)
		})
	}
}

func (*Server) isMediaFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	mediaExts := []string{".mp3", ".wav", ".flac", ".aac", ".ogg", ".m4a", ".wma", ".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm", ".m4v", ".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".bmp", ".tiff"}
	return slices.Contains(mediaExts, ext)
}

func (s *Server) getRandomMediaFile(relativePath string) (string, error) {
	data, err := s.getDirectoryListing(relativePath)
	if err != nil {
		return "", err
	}
	var mediaFiles []string
	for _, file := range data.Files {
		if !file.IsDir && s.isMediaFile(file.Name) {
			mediaFiles = append(mediaFiles, file.Path)
		}
	}
	if len(mediaFiles) == 0 {
		return "", errors.New("no media files found")
	}
	return mediaFiles[rand.Intn(len(mediaFiles))], nil
}

func (*Server) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[%s] %s %s - %s", start.Format("2006-01-02 15:04:05"), r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	err := s.template.Execute(w, nil)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/browse/")
	if relativePath != "" && relativePath != "/" {
		var err error
		relativePath, err = url.PathUnescape(relativePath)
		if err != nil {
			http.Error(w, "Invalid path", http.StatusBadRequest)
			return
		}
	} else {
		relativePath = ""
	}
	err := s.template.Execute(w, nil)
	if err != nil {
		http.Error(w, "Error rendering template", http.StatusInternalServerError)
		log.Printf("Template error: %v", err)
	}
}

func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request) {
	relativePath := r.URL.Query().Get("path")
	data, err := s.getDirectoryListing(relativePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sortBy := r.URL.Query().Get("sort")
	order := r.URL.Query().Get("order")
	if sortBy != "" {
		s.sortFiles(data.Files, sortBy, order)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) handleRandomMedia(w http.ResponseWriter, r *http.Request) {
	relativePath := r.URL.Query().Get("path")
	mediaFile, err := s.getRandomMediaFile(relativePath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte(mediaFile))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()
	s.clients[conn] = true
	defer delete(s.clients, conn)
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/files/")
	relativePath, err := url.PathUnescape(relativePath)
	if err != nil {
		http.Error(w, "Invalid path", http.StatusBadRequest)
		return
	}
	path := filepath.Clean(relativePath)
	if strings.Contains(path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	fullPath := filepath.Join(s.rootDir, path)
	absRoot, _ := filepath.Abs(s.rootDir)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	http.ServeFile(w, r, fullPath)
}

func main() {
	port := flag.String("port", "8080", "Port to listen on")
	dir := flag.String("dir", "", "Directory to serve (default: current directory)")
	flag.Parse()

	rootDir := *dir
	if rootDir == "" {
		var err error
		rootDir, err = os.Getwd()
		if err != nil {
			log.Fatal("Error getting current directory:", err)
		}
	}
	if _, err := os.Stat(rootDir); os.IsNotExist(err) {
		log.Fatal("Directory does not exist:", rootDir)
	}

	server, err := NewServer(rootDir)
	if err != nil {
		log.Fatal("Error creating server:", err)
	}
	defer server.watcher.Close()

	mux := http.NewServeMux()

	staticContentFS, err := fs.Sub(staticFilesystem, "static")
	if err != nil {
		log.Fatal("failed to create sub static content FS:", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContentFS))))

	mux.HandleFunc("/", server.handleIndex)
	mux.HandleFunc("/browse/", server.handleBrowse)
	mux.HandleFunc("/api/files", server.handleAPI)
	mux.HandleFunc("/api/random-media", server.handleRandomMedia)
	mux.HandleFunc("/ws", server.handleWebSocket)
	mux.HandleFunc("/files/", server.handleFiles)

	handler := server.logRequest(mux)

	log.Printf("Starting server on port %s, serving directory: %s", *port, rootDir)
	log.Printf("Access the server at: http://localhost:%s", *port)
	log.Printf("LAN access available at: http://<your-ip>:%s", *port)

	err = http.ListenAndServe("0.0.0.0:"+*port, handler)
	if err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
