package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/brandur/modulir"
	"github.com/brandur/modulir/modules/mfile"
)

//////////////////////////////////////////////////////////////////////////////
//
//
//
// Main
//
//
//
//////////////////////////////////////////////////////////////////////////////

func main() {
	goStr := goHeader

	sources, err := mfile.ReadDir(newContext(), jsSource)
	if err != nil {
		exitWithError(err)
	}

	for _, source := range sources {
		file := filepath.Base(source)
		name := strings.TrimSuffix(file, filepath.Ext(file))

		data, err := ioutil.ReadFile(source)
		if err != nil {
			exitWithError(err)
		}

		str := string(data)
		str = strings.ReplaceAll(str, `"`, `\"`)
		str = strings.ReplaceAll(str, "\n", "\\n\" +\n\t\"");

		goStr += fmt.Sprintf(goTemplate, file, name, str)
	}

	if err := ioutil.WriteFile(goTarget, []byte(goStr), 0755); err != nil {
		exitWithError(err)
	}
}

//////////////////////////////////////////////////////////////////////////////
//
//
//
// Private
//
//
//
//////////////////////////////////////////////////////////////////////////////

// The header frontmatter that will go in our generate .go file.
const goHeader = `//
//
// Code generated by: scripts/embed_js/main.go
// DO NOT EDIT. Run go generate instead.
//
//

package modulir`

// Target Go file to generate containing JS sources.
const goTarget = "./js.go"

// Go code for each file. Note that we have leading newlines instead of
// trailing so that Gofmt doesn't have to change anything.
const goTemplate = `

// Source: %s
const %sJS = "%s"`

// Directory containing JavaScript sources.
const jsSource = "./js"

// Exits with status 1 after printing the given error to stderr.
func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

// Helper to easily create a new Modulir context.
func newContext() *modulir.Context {
	return modulir.NewContext(&modulir.Args{Log: &modulir.Logger{Level: modulir.LevelInfo}})
}
