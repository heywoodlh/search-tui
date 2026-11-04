// Command search is a terminal client for web and code search engines.
package main

import (
	"os"

	"github.com/heywoodlh/search-tui/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
