package main

import "embed"

//go:embed all:templates
var templateFS embed.FS

//go:embed all:static
var staticFilesystem embed.FS
