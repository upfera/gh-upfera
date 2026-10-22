package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommandContextRunsTrustedExecutable(t *testing.T) {
	command, err := commandContext(context.Background(), "git", "--version")
	if err != nil {
		t.Fatalf("commandContext() error = %v", err)
	}
	output, err := command.Output()
	if err != nil {
		t.Fatalf("git --version error = %v", err)
	}
	if !strings.HasPrefix(string(output), "git version ") {
		t.Fatalf("git --version output = %q", output)
	}
}

func TestCommandContextRejectsMissingExecutable(t *testing.T) {
	directory := t.TempDir()
	t.Setenv("PATH", directory)
	_, err := commandContext(context.Background(), "gh-upfera-missing-test-command")
	if err == nil || !strings.Contains(err.Error(), "find gh-upfera-missing-test-command executable") {
		t.Fatalf("commandContext() error = %v, want missing executable", err)
	}
}

func TestValidateExecutablePathRejectsWritableDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "writable")
	if err := os.Mkdir(directory, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o770); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "tool")
	if err := os.WriteFile(binary, []byte("tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutablePath(binary); err == nil || !strings.Contains(err.Error(), "writable by other users") {
		t.Fatalf("validateExecutablePath() error = %v, want writable directory error", err)
	}
}

func TestValidateExecutablePathAcceptsRegularFile(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(directory, "tool")
	if err := os.WriteFile(binary, []byte("tool"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutablePath(binary); err != nil {
		t.Fatalf("validateExecutablePath() error = %v", err)
	}
}

func TestValidateExecutablePathRejectsDirectoriesAndWritableExecutables(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutablePath(directory); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("validateExecutablePath(directory) error = %v", err)
	}

	binary := filepath.Join(directory, "writable-tool")
	if err := os.WriteFile(binary, []byte("tool"), 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binary, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := validateExecutablePath(binary); err == nil || !strings.Contains(err.Error(), "writable by other users") {
		t.Fatalf("validateExecutablePath(writable file) error = %v", err)
	}
}

func TestCommandContextResolvesExecutableSymlink(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "real-tool")
	if err := os.WriteFile(target, []byte(`#!/bin/sh
printf resolved
`), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "tool")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	command, err := commandContext(context.Background(), "tool")
	if err != nil {
		t.Fatalf("commandContext() error = %v", err)
	}
	output, err := command.Output()
	if err != nil || string(output) != "resolved" {
		t.Fatalf("tool output/error = %q / %v", output, err)
	}
}

func TestSetEnvironmentReplacesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"KEEP=value", "GH_TOKEN=old", "GH_TOKEN=duplicate"}, "GH_TOKEN", "new")
	if len(got) != 2 || got[0] != "KEEP=value" || got[1] != "GH_TOKEN=new" {
		t.Fatalf("setEnvironment() = %#v", got)
	}
}
