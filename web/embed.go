package web

import "embed"

//go:embed dist/index.html dist/assets/*
var DistFiles embed.FS
