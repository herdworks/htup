// Command htdt is the HyperTexting Deploy Tool — a standalone CLI for
// deploying arbitrary directories to S3 or Git via hyperproviders-go.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

const version = "1.0.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "deploy":
		err = runDeploy(ctx, args)
	case "validate":
		err = runValidate(ctx, args)
	case "ready":
		err = runReady(ctx, args)
	case "list":
		err = runList(ctx, args)
	case "version", "--version", "-v":
		fmt.Println(version)
		return
	case "help", "--help", "-h":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `htdt — HyperTexting Deploy Tool

Usage:
  htdt deploy    --config <file> --environment <name> --source <path> --mode <replacement|overlay> [--dry-run]
  htdt validate  --config <file>
  htdt ready     --config <file> [--environment <name>]
  htdt list      --config <file>
  htdt version`)
}
