package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
)

const (
	sessionTokenBytes = 32
	sessionCookieName = "filebrowser_session_token"
)

type Server struct {
	rootDir        string
	upgrader       websocket.Upgrader
	clients        map[*websocket.Conn]bool
	watcher        *fsnotify.Watcher
	broadcast      chan []byte
	template       *template.Template // For index.html
	loginTemplate  *template.Template // For login.html
	authEnabled    bool
	randomBtn      bool
	hashedPassword []byte
	sessions       map[string]time.Time // session token -> creation time
}

func NewServer(rootDir string, password string, enableRandomBtn bool) (*Server, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Parse index.html
	indexTmpl, err := template.ParseFS(templateFS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded index.html template: %w", err)
	}

	// Parse login.html
	loginTmpl, err := template.ParseFS(templateFS, "templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("failed to load embedded login.html template: %w", err)
	}

	server := &Server{
		rootDir:       rootDir,
		template:      indexTmpl,
		loginTemplate: loginTmpl,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
		clients:   make(map[*websocket.Conn]bool),
		watcher:   watcher,
		broadcast: make(chan []byte),
		randomBtn: enableRandomBtn,
	}

	if password != "" {
		hashedPass, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, fmt.Errorf("failed to hash password: %w", err)
		}
		server.authEnabled = true
		server.hashedPassword = hashedPass
		server.sessions = make(map[string]time.Time)
		log.Println("Password protection enabled.")
	} else {
		log.Println("Password protection disabled.")
	}

	err = server.watchDirectory()
	if err != nil {
		if watcherErr := watcher.Close(); watcherErr != nil {
			log.Printf("Error closing watcher after NewServer setup failed: %v", watcherErr)
		}
		return nil, fmt.Errorf("failed to start watching directory: %w", err)
	}

	go server.handleBroadcast()

	return server, nil
}

func (s *Server) generateSessionToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (s *Server) addSession(token string) {
	s.sessions[token] = time.Now()
}

func (s *Server) isValidSession(token string) bool {
	_, exists := s.sessions[token]
	return exists
}

func (s *Server) removeSession(token string) {
	delete(s.sessions, token)
}

// watchDirectory and handleBroadcast methods remain the same as your provided code.
func (s *Server) watchDirectory() error {
	err := s.watcher.Add(s.rootDir)
	if err != nil {
		return fmt.Errorf("failed to add root directory to watcher: %w", err)
	}

	err = filepath.WalkDir(s.rootDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("Error walking path %s: %v", path, walkErr)
			return nil
		}
		if d.IsDir() {
			if addErr := s.watcher.Add(path); addErr != nil {
				log.Printf("Failed to add subdirectory %s to watcher: %v", path, addErr)
			}
		}
		return nil
	})
	if err != nil {
		return errors.New("error walking dir")
	}

	go func() {
		for {
			select {
			case event, ok := <-s.watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Create == fsnotify.Create {
					if info, statErr := os.Stat(event.Name); statErr == nil && info.IsDir() {
						if addErr := s.watcher.Add(event.Name); addErr != nil {
							log.Printf("Failed to add newly created directory %s to watcher: %v", event.Name, addErr)
						}
					}
				}
				updateMsg := map[string]any{"type": "update"}
				jsonData, marshalErr := json.Marshal(updateMsg)
				if marshalErr != nil {
					log.Printf("Error marshalling update message: %v", marshalErr)
					continue
				}
				s.broadcast <- jsonData
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
	for data := range s.broadcast {
		for client := range s.clients {
			err := client.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				log.Printf("Error writing message to client %s: %v", client.RemoteAddr(), err)
				client.Close()
				delete(s.clients, client)
			}
		}
	}
}
