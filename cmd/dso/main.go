package main

import "github.com/docker-secret-operator/dso/internal/cli"

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	cli.Version = version
	cli.Commit = commit
	cli.BuildDate = date
	cli.Execute()
}
