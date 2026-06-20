// Command sagittarius is the Sagittarius CLI — a Go port of the gemini-cli fork.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/undeadindustries/sagittarius/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	showVersionShort := flag.Bool("v", false, "print version and exit")
	flag.Parse()

	if *showVersion || *showVersionShort {
		fmt.Println(version.String())
		os.Exit(0)
	}

	os.Exit(0)
}
