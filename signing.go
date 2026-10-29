package main

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func checkSigningKey(ctx context.Context) error {
	if err := checkGitConfiguration(ctx, "user.name"); err != nil {
		return fmt.Errorf("configure a GitHub-verified signing key and Git author identity before creating repositories")
	}
	if err := checkGitConfiguration(ctx, "user.email"); err != nil {
		return fmt.Errorf("configure a GitHub-verified signing key and Git author identity before creating repositories")
	}

	signingKey, err := gitConfig(ctx, "user.signingkey")
	if err != nil || signingKey == "" {
		return fmt.Errorf("configure a GitHub-verified signing key and Git author identity before creating repositories")
	}
	format, _ := gitConfig(ctx, "gpg.format")
	if format == "" {
		format = "openpgp"
	}
	if format == "ssh" {
		if !strings.HasPrefix(signingKey, "key::") {
			if _, err := os.Stat(signingKey); err != nil {
				return fmt.Errorf("configure a GitHub-verified signing key and Git author identity before creating repositories")
			}
		}
	}
	if format == "openpgp" {
		gpgProgram, _ := gitConfig(ctx, "gpg.program")
		if gpgProgram == "" {
			gpgProgram = "gpg"
		}
		command, err := commandContext(ctx, gpgProgram, "--batch", "--list-secret-keys", signingKey)
		if err != nil || command.Run() != nil {
			return fmt.Errorf("configure a GitHub-verified signing key and Git author identity before creating repositories")
		}
	}
	return nil
}

func checkGitConfiguration(ctx context.Context, key string) error {
	value, err := gitConfig(ctx, key)
	return errOrEmpty(value, err)
}

func gitConfig(ctx context.Context, key string) (string, error) {
	command, err := commandContext(ctx, "git", "config", "--get", key)
	if err != nil {
		return "", err
	}
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func errOrEmpty(value string, err error) error {
	if err != nil || value == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("configuration is empty")
	}
	return nil
}

func (c *gitHubClient) createSignedRootCommit(ctx context.Context, repo, branch, treeSHA string) (string, string, error) {
	directory, err := os.MkdirTemp("", "gh-upfera-*")
	if err != nil {
		return "", "", fmt.Errorf("create temporary directory: %w", err)
	}
	defer os.RemoveAll(directory)

	clone, err := commandContext(ctx, "gh", "repo", "clone", upferaOwner+"/"+repo, directory, "--", "--depth=1", "--branch", branch)
	if err != nil {
		return "", "", err
	}
	clone.Env = setEnvironment(os.Environ(), "GH_TOKEN", c.token)
	if err := clone.Run(); err != nil {
		return "", "", fmt.Errorf("clone the generated repository; verify GitHub CLI authentication")
	}

	commit, err := createSignedRootCommit(ctx, directory, treeSHA)
	if err != nil {
		return "", "", err
	}
	bootstrapBranch := "gh-upfera-bootstrap-" + commit[:12]
	push, err := commandContext(ctx, "git", "-C", directory, "-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential", "push", "origin", commit+":refs/heads/"+bootstrapBranch)
	if err != nil {
		return "", "", err
	}
	push.Env = setEnvironment(os.Environ(), "GH_TOKEN", c.token)
	if err := push.Run(); err != nil {
		return "", "", fmt.Errorf("push the signed initial commit to a temporary branch; verify GitHub permissions")
	}
	return commit, bootstrapBranch, nil
}

func createSignedRootCommit(ctx context.Context, directory, treeSHA string) (string, error) {
	command, err := commandContext(ctx, "git", "-C", directory, "commit-tree", "-S", treeSHA, "-F", "-")
	if err != nil {
		return "", err
	}
	command.Stdin = strings.NewReader(initialCommitMessage + "\n")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("create the signed initial commit; configure a GitHub-verified signing key and Git identity")
	}
	commit := strings.TrimSpace(string(output))
	if len(commit) != 40 {
		return "", fmt.Errorf("Git returned an invalid signed commit identifier")
	}
	return commit, nil
}

func setEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}
