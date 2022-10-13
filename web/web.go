// Package web contains the files for the Northstar website.
package web

import (
	"embed"
	"net/http"
	"strings"
	"time"
)

//go:embed index.html assets/* script/* style/*
var Assets embed.FS

// TODO: compress assets
// TODO: probably better to put website in a separate repo

var Redirects = map[string]string{
	"/github":       "https://github.com/R2Northstar",
	"/discord":      "https://discord.gg/northstar",
	"/wiki":         "https://r2northstar.gitbook.io/",
	"/thunderstore": "https://northstar.thunderstore.io",
}

// TODO: probably better to make redirects configurable via a JSON file or something

func ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// - cache publicly, allow reusing responses for multiple users
	// - allow reusing responses if server is down
	// - cache for up to 1800s
	// - check for updates after 900s
	w.Header().Set("Cache-Control", "public, max-age=1800, stale-while-revalidate=900")
	w.Header().Set("Expires", time.Now().UTC().Add(time.Second*1800).Format(http.TimeFormat))

	// check redirects first
	if u, ok := Redirects[strings.TrimSuffix(r.URL.Path, "/")]; ok {
		http.Redirect(w, r, u, http.StatusTemporaryRedirect)
		return
	}

	// this handles range requests, etags, time, etc
	http.FileServer(http.FS(Assets)).ServeHTTP(w, r)
}
