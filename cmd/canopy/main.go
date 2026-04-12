package main

import "os"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		exitCode := 1
		if withCode, ok := err.(interface{ ExitCode() int }); ok {
			exitCode = withCode.ExitCode()
		}
		os.Exit(exitCode)
	}
}
