package web

import "embed"

//go:embed dist/*
var DistFiles embed.FS
