package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/odvcencio/gts-suite/pkg/lsp"
	"github.com/odvcencio/gts-suite/pkg/socket"
)

var version = "0.1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println("gtsls " + version)
		os.Exit(0)
	}

	// Client mode: gtsls client <method> [params...]
	if len(os.Args) > 2 && os.Args[1] == "client" {
		runClient(os.Args[2:])
		return
	}

	// Detect gopackagesdriver mode: called with patterns as args
	if len(os.Args) > 1 && os.Args[1] != "--stdio" {
		cwd, _ := os.Getwd()
		if err := lsp.RunDriver(cwd, os.Args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "gtsls driver: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// LSP mode (default)
	svc := lsp.NewService(nil)
	srv := lsp.NewServer(os.Stdin, os.Stdout, os.Stderr)
	svc.Register(srv)

	if err := srv.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "gtsls: %v\n", err)
		os.Exit(1)
	}
}

func runClient(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: gtsls client <method> [param=value ...]")
		os.Exit(1)
	}
	method := args[0]

	// Build params from remaining args as key=value pairs
	params := make(map[string]string)
	for _, arg := range args[1:] {
		if i := indexOf(arg, '='); i >= 0 {
			params[arg[:i]] = arg[i+1:]
		} else {
			// Positional: first positional is "symbol" or "file"
			if _, exists := params["symbol"]; !exists {
				params["symbol"] = arg
			} else {
				params["file"] = arg
			}
		}
	}

	cwd, _ := os.Getwd()
	client, err := socket.Dial(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	var callParams any
	if len(params) > 0 {
		callParams = params
	}

	result, err := client.Call(method, callParams)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Pretty-print JSON
	var pretty json.RawMessage
	if json.Unmarshal(result, &pretty) == nil {
		out, _ := json.MarshalIndent(pretty, "", "  ")
		fmt.Println(string(out))
	} else {
		fmt.Println(string(result))
	}
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
