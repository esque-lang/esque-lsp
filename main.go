// esque-lsp is a Language Server Protocol implementation for the
// esque programming language (https://github.com/esque-lang/esquec).
//
// It speaks LSP 3.17 over stdin/stdout using the standard
// Content-Length framed JSON-RPC 2.0 transport.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	flagVersion = flag.Bool("version", false, "print version and exit")
	flagLog     = flag.String("log", "", "path to a log file (default: stderr)")
)

const version = "0.1.0"

func main() {
	flag.Parse()
	if *flagVersion {
		fmt.Printf("esque-lsp %s\n", version)
		return
	}

	if *flagLog != "" {
		f, err := os.OpenFile(*flagLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cannot open log: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		log.SetOutput(f)
	} else {
		log.SetOutput(os.Stderr)
	}
	log.SetPrefix("[esque-lsp] ")
	log.SetFlags(log.Ltime | log.Lmicroseconds)

	srv := NewServer()
	if err := srv.Run(os.Stdin, os.Stdout); err != nil {
		log.Fatalf("server exited with error: %v", err)
	}
}
