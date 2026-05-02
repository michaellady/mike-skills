package main

import (
	"os"

	"github.com/michaellady/mike-skills/converge/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
