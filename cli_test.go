package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseCreateArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want createOptions
	}{
		{
			name: "private by default",
			args: []string{"upfera-automations", "--description", "Automation definitions"},
			want: createOptions{Name: "upfera-automations", Description: "Automation definitions", Visibility: "private"},
		},
		{
			name: "flags can precede the positional argument",
			args: []string{"--public", "-d", "Public repo", "automation-tools"},
			want: createOptions{Name: "automation-tools", Description: "Public repo", Visibility: "public"},
		},
		{
			name: "equals options",
			args: []string{"repo", "--visibility=public", "--description=Some text"},
			want: createOptions{Name: "repo", Description: "Some text", Visibility: "public"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCreateArgs(tt.args)
			if err != nil {
				t.Fatalf("parseCreateArgs() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseCreateArgs() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseCreateArgsRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing name", args: []string{"--public"}, want: "exactly one repository name"},
		{name: "too many names", args: []string{"one", "two"}, want: "exactly one repository name"},
		{name: "invalid visibility", args: []string{"one", "--visibility", "internal"}, want: "visibility must be public or private"},
		{name: "conflicting visibility flags", args: []string{"one", "--public", "--private"}, want: "specify visibility only once"},
		{name: "unknown option", args: []string{"one", "--force"}, want: "unknown option"},
		{name: "missing description", args: []string{"one", "--description"}, want: "requires a value"},
		{name: "slashes in name", args: []string{"owner/repo"}, want: "invalid character"},
		{name: "consecutive dots", args: []string{"bad..name"}, want: "consecutive dots"},
		{name: "git suffix", args: []string{"repo.GIT"}, want: "cannot end with .git"},
		{name: "control character in description", args: []string{"one", "-d", "bad\ndescription"}, want: "control characters"},
		{name: "description over limit", args: []string{"one", "-d", strings.Repeat("é", 351)}, want: "350 characters"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseCreateArgs(tt.args)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseCreateArgs() error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestRunHelpDoesNotRequireAuthentication(t *testing.T) {
	var stdout, stderr strings.Builder
	if err := run(context.Background(), []string{"repo", "create", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("help output = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunMainReturnsExitStatus(t *testing.T) {
	var stdout, stderr strings.Builder
	if code := runMain(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("runMain(--help) = %d, want 0", code)
	}
	if code := runMain(context.Background(), []string{"invalid"}, &stdout, &stderr); code != 1 {
		t.Fatalf("runMain(invalid) = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "gh-upfera: unknown command") {
		t.Fatalf("error output = %q", stderr.String())
	}
}

func TestRunInvalidCommandAndClientFailure(t *testing.T) {
	var stdout, stderr strings.Builder
	factory := func(context.Context) (*gitHubClient, error) {
		return nil, fmt.Errorf("client setup failed")
	}
	if err := runWithClientFactory(context.Background(), nil, &stdout, &stderr, factory); err != nil {
		t.Fatalf("run() with no arguments error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("help output = %q", stdout.String())
	}
	if err := runWithClientFactory(context.Background(), []string{"repo", "delete"}, &stdout, &stderr, factory); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("run() with invalid command error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("invalid-command output = %q", stderr.String())
	}
	if err := runWithClientFactory(context.Background(), []string{"repo", "create", "sample"}, &stdout, &stderr, factory); err == nil || !strings.Contains(err.Error(), "client setup failed") {
		t.Fatalf("run() with client error = %v", err)
	}
}

func TestRunCreatesRepositoryAndPrintsSettings(t *testing.T) {
	server := newRunTestServer()
	defer server.Close()
	client := &gitHubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
	var stdout, stderr strings.Builder
	factory := func(context.Context) (*gitHubClient, error) { return client, nil }

	if err := runWithClientFactory(context.Background(), []string{"repo", "create", "sample"}, &stdout, &stderr, factory); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Created https://github.com/upfera/sample") || !strings.Contains(stdout.String(), "Verified squash merge: enabled") {
		t.Fatalf("success output = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func newRunTestServer() *httptest.Server {
	settingsApplied := false
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/upfera/sample":
			if !settingsApplied {
				writeJSON(w, http.StatusNotFound, apiError{Message: "Not Found"})
				return
			}
			writeJSON(w, http.StatusOK, repository{HTMLURL: "https://github.com/upfera/sample", AllowSquashMerge: true, SquashMergeCommitTitle: squashTitle, SquashMergeCommitMessage: squashMessage})
		case "POST /repos/upfera/.template/generate":
			writeJSON(w, http.StatusCreated, repository{Name: "sample", HTMLURL: "https://github.com/upfera/sample", DefaultBranch: "main"})
		case "GET /repos/upfera/sample/git/ref/heads/main":
			writeJSON(w, http.StatusOK, map[string]any{"object": map[string]string{"sha": "root"}})
		case "GET /repos/upfera/sample/git/commits/root":
			writeJSON(w, http.StatusOK, map[string]any{"message": initialCommitMessage, "tree": map[string]string{"sha": "template"}, "parents": []any{}, "verification": map[string]bool{"verified": true}})
		case "PATCH /repos/upfera/sample":
			settingsApplied = true
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, `{"message":"unexpected request"}`, http.StatusInternalServerError)
		}
	}))
}
