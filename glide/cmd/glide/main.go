// glide runs Glide programs: glide run <file.gld> [args...]
package main

import (
	"errors"
	"fmt"
	"os"

	"glide/internal/check"
	"glide/internal/interp"
	"glide/internal/parser"
	"glide/internal/program"
	"glide/internal/source"
)

const usage = `usage: glide run <file.gld> [args...]
       glide test <file.gld>
       glide check <file.gld>`

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	mode, path := os.Args[1], os.Args[2]
	switch mode {
	case "run", "test", "check":
	default:
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	src, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "glide: %v\n", err)
		os.Exit(1)
	}
	file, err := parser.ParseFile(path, string(src))
	if err != nil {
		// A parse error already knows its own file, line, column and
		// source line — it renders itself.
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// `glide check` is go-vet-shaped: report and stop. It is a
	// convenience, never a way to *skip* checking — run and test check
	// too, and there is no flag that turns that off.
	if mode == "check" {
		tab, err := program.Load(file, check.Host())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if _, err := check.File(file, tab); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
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
		// A positioned error already reads as "file:line:col: msg";
		// prefixing "error:" onto that just pushes the path off the
		// left edge where editors look for it.
		var se *source.Error
		if errors.As(err, &se) {
			fmt.Fprintln(os.Stderr, se)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}
