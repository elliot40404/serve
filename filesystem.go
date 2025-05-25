package main

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

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

func getDirectoryListing(rootDir string, relativePath string) (*DirectoryData, error) {
	cleanPath := filepath.Clean(relativePath)
	if cleanPath == "." {
		cleanPath = ""
	}
	if strings.Contains(cleanPath, "..") {
		return nil, errors.New("invalid path")
	}
	fullPath := filepath.Join(rootDir, cleanPath)

	absRoot, _ := filepath.Abs(rootDir)
	absPath, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absPath, absRoot) {
		return nil, errors.New("path outside root directory")
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	files := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		var filePath string
		if info.IsDir() {
			if cleanPath == "" {
				filePath = "/browse/" + url.PathEscape(info.Name())
			} else {
				filePath = "/browse/" + url.PathEscape(cleanPath) + "/" + url.PathEscape(info.Name())
			}
		} else {
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
			Path:    filePath, // This path is used by the frontend to construct links
		})
	}

	var parentPathHREF string // This will be the href attribute for the ".." link
	var hasParent bool
	if cleanPath != "" {
		// For the backend, parentDir is the logical parent.
		// For the frontend's ".." link, it needs the logical parent to put in data-path.
		// The DirectoryData.ParentPath field here is for historical reasons/direct href use if any.
		// Frontend recalculates logical parent for data-path anyway.
		parentDirLogical := filepath.Dir(cleanPath)
		if parentDirLogical == "." { // Parent of "a" is "."
			parentPathHREF = "/browse/" // Root browse path
		} else {
			parentPathHREF = "/browse/" + url.PathEscape(parentDirLogical)
		}
		hasParent = true
	}

	return &DirectoryData{
		Files:       files,
		CurrentPath: cleanPath,
		ParentPath:  parentPathHREF, // Used if frontend directly makes an href from this
		HasParent:   hasParent,
	}, nil
}

func sortFiles(files []FileInfo, sortBy, order string) {
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

func isMediaFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	mediaExts := []string{
		".mp3",
		".wav",
		".flac",
		".aac",
		".ogg",
		".m4a",
		".wma",
		".mp4",
		".avi",
		".mkv",
		".mov",
		".wmv",
		".flv",
		".webm",
		".m4v",
		".jpg",
		".jpeg",
		".png",
		".gif",
		".webp",
		".svg",
		".bmp",
		".tiff",
	}
	return slices.Contains(mediaExts, ext)
}

func getRandomMediaFile(rootDir string, relativePath string) (string, error) {
	data, err := getDirectoryListing(rootDir, relativePath)
	if err != nil {
		return "", err
	}

	var mediaFiles []string
	for _, file := range data.Files {
		if !file.IsDir && isMediaFile(file.Name) {
			// file.Path from getDirectoryListing is already the correct /files/... path
			mediaFiles = append(mediaFiles, file.Path)
		}
	}

	if len(mediaFiles) == 0 {
		return "", errors.New("no media files found")
	}

	n := big.NewInt(int64(len(mediaFiles)))
	i, _ := rand.Int(rand.Reader, n)
	return mediaFiles[i.Int64()], nil
}
