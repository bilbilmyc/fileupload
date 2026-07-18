package main

import "fmt"

var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func versionString() string {
	return fmt.Sprintf("%s (commit %s, built %s)", version, commit, builtAt)
}
