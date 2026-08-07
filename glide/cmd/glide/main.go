// glide runs Glide programs: glide run <file.gl> [args...]
package main

import (
	"errors"
	"fmt"
	"os"

	"glide/internal/interp"
	"glide/internal/parser"
)

func main() {
	if len(os.Args) < 3 || os.Args[1] != "run" {
		fmt.Fprintln(os.Stderr, "usage: glide run <file.gl> [args...]")
		os.Exit(2)
	}
	path := os.Args[2]
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glide: %v\n", err)
		os.Exit(1)
	}
	file, err := parser.ParseFile(string(src))
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
		os.Exit(1)
	}
	in := interp.New()
	in.Args = append([]string{path}, os.Args[3:]...)
	if err := in.Run(file); err != nil {
		var exit *interp.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.Code)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
