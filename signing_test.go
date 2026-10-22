package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateSignedRootCommitProducesSingleSignedCommit(t *testing.T) {
	directory := t.TempDir()
	command(t, directory, "git", "init", "-q")
	command(t, directory, "git", "config", "user.name", "Test User")
	command(t, directory, "git", "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(directory, "template.txt"), []byte("template content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	command(t, directory, "git", "add", "template.txt")
	command(t, directory, "git", "commit", "-q", "-m", "template baseline")
	treeSHA := command(t, directory, "git", "rev-parse", "HEAD^{tree}")

	keyPath := filepath.Join(directory, "signing-key")
	command(t, directory, "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test@example.com", "-f", keyPath)
	command(t, directory, "git", "config", "gpg.format", "ssh")
	command(t, directory, "git", "config", "user.signingkey", keyPath)

	commitSHA, err := createSignedRootCommit(context.Background(), directory, treeSHA)
	if err != nil {
		t.Fatalf("createSignedRootCommit() error = %v", err)
	}

	commitContent := command(t, directory, "git", "cat-file", "-p", commitSHA)
	if strings.Contains(commitContent, "\nparent ") {
		t.Fatalf("root commit unexpectedly has a parent: %s", commitContent)
	}
	if !strings.Contains(commitContent, "\ngpgsig -----BEGIN SSH SIGNATURE-----") {
		t.Fatal("commit has no SSH signature")
	}
	if !strings.HasSuffix(strings.TrimSpace(commitContent), initialCommitMessage) {
		t.Fatalf("commit message = %q", commitContent)
	}

	allowedSigners := filepath.Join(directory, "allowed_signers")
	publicKey, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(allowedSigners, []byte("test@example.com "+string(publicKey)), 0o600); err != nil {
		t.Fatal(err)
	}
	command(t, directory, "git", "-c", "gpg.ssh.allowedSignersFile="+allowedSigners, "verify-commit", commitSHA)
}

func TestCheckSigningKeyUsesConfiguredSSHKey(t *testing.T) {
	configuration := filepath.Join(t.TempDir(), "gitconfig")
	keyPath := filepath.Join(t.TempDir(), "signing-key")
	t.Setenv("GIT_CONFIG_GLOBAL", configuration)
	command(t, t.TempDir(), "ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-C", "test@example.com", "-f", keyPath)
	command(t, t.TempDir(), "git", "config", "--global", "user.name", "Test User")
	command(t, t.TempDir(), "git", "config", "--global", "user.email", "test@example.com")
	command(t, t.TempDir(), "git", "config", "--global", "gpg.format", "ssh")
	command(t, t.TempDir(), "git", "config", "--global", "user.signingkey", keyPath)

	if err := checkSigningKey(context.Background()); err != nil {
		t.Fatalf("checkSigningKey() error = %v", err)
	}
}

func TestCheckSigningKeyRejectsUnusableSSHKey(t *testing.T) {
	directory := t.TempDir()
	configuration := filepath.Join(directory, "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", configuration)
	command(t, directory, "git", "config", "--global", "user.name", "Test User")
	command(t, directory, "git", "config", "--global", "user.email", "test@example.com")
	command(t, directory, "git", "config", "--global", "gpg.format", "ssh")
	command(t, directory, "git", "config", "--global", "user.signingkey", filepath.Join(directory, "missing-key"))

	if err := checkSigningKey(context.Background()); err == nil || !strings.Contains(err.Error(), "GitHub-verified signing key") {
		t.Fatalf("checkSigningKey() error = %v, want unusable key error", err)
	}
}

func TestCreateSignedRootCommitReportsGitFailure(t *testing.T) {
	if _, err := createSignedRootCommit(context.Background(), t.TempDir(), "invalid-tree"); err == nil || !strings.Contains(err.Error(), "create the signed initial commit") {
		t.Fatalf("createSignedRootCommit() error = %v, want Git error", err)
	}
}

func command(t *testing.T, directory string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, output)
	}
	return strings.TrimSpace(string(output))
}
