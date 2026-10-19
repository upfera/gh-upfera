package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if err := run(ctx, args, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, "gh-upfera:", err)
		return 1
	}
	return 0
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return runWithClientFactory(ctx, args, stdout, stderr, newGitHubClient)
}

func runWithClientFactory(ctx context.Context, args []string, stdout, stderr io.Writer, newClient func(context.Context) (*gitHubClient, error)) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" {
		printUsage(stdout)
		return nil
	}
	if len(args) < 2 || args[0] != "repo" || args[1] != "create" {
		printUsage(stderr)
		return fmt.Errorf("unknown command")
	}

	if len(args) == 3 && (args[2] == "--help" || args[2] == "-h") {
		printUsage(stdout)
		return nil
	}
	options, err := parseCreateArgs(args[2:])
	if err != nil {
		if err.Error() == "help requested" {
			printUsage(stdout)
			return nil
		}
		return err
	}

	client, err := newClient(ctx)
	if err != nil {
		return err
	}
	repository, err := client.CreateRepository(ctx, options)
	if err != nil {
		return err
	}

	fmt.Fprintf(stdout, "Created %s\n", repository.HTMLURL)
	fmt.Fprintln(stdout, "Verified squash merge: enabled (PR_TITLE / PR_BODY)")
	fmt.Fprintln(stdout, "Verified merge commits and rebase merging: disabled")
	return nil
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Usage:
  gh upfera repo create <name> [--description <text>] [--visibility public|private]

Create a repository from upfera/.template and apply Upfera repository defaults.

Options:
  -d, --description <text>    Repository description (up to 350 characters)
      --visibility <value>    Repository visibility: public or private (default: private)
      --public                Create a public repository
      --private               Create a private repository
  -h, --help                  Show this help`)
}
