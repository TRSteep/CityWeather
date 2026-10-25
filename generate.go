package main

// Artwork and Windows resources are committed, so ordinary builds work offline.
//go:generate go run ./internal/icon
//go:generate go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico assets/app.ico -o resources_windows_amd64.syso
