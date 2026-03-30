package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: drydock <command> [args]")
		fmt.Fprintln(os.Stderr, "commands: validate, run, destroy, inspect")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "validate":
		fmt.Println("validate: not yet implemented")
	case "run":
		fmt.Println("run: not yet implemented")
	case "destroy":
		fmt.Println("destroy: not yet implemented")
	case "inspect":
		fmt.Println("inspect: not yet implemented")
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		os.Exit(1)
	}
}
