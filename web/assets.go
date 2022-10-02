// Package web contains the files for the Northstar website.
package web

import "embed"

//go:embed index.html assets/* script/* style/*
var Assets embed.FS
