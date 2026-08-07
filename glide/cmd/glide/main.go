// glide runs Glide programs: glide run <file.gld> [args...]
package main

import (
	"errors"
	"fmt"
	"os"

	"glide/internal/interp"
	"glide/internal/parser"
)

func main() {
	if len(os.Args) < 3 || (os.Args[1] != "run" && os.Args[1] != "test") {
		fmt.Fprintln(os.Stderr, "usage: glide run <file.gld> [args...]\n       glide test <file.gld>")
		os.Exit(2)
	}
	mode, path := os.Args[1], os.Args[2]
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
	if mode == "test" {
		if failed := interp.RunTests(file, os.Stdout); failed > 0 {
			os.Exit(1)
		}
		return
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
