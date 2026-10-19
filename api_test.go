package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewGitHubClientUsesConfiguredTokenAndEnterpriseHost(t *testing.T) {
	t.Setenv("GH_TOKEN", "preferred-token")
	t.Setenv("GITHUB_TOKEN", "fallback-token")
	t.Setenv("GH_HOST", "github.example.com")

	client, err := newGitHubClient(context.Background())
	if err != nil {
		t.Fatalf("newGitHubClient() error = %v", err)
	}
	if client.token != "preferred-token" || client.baseURL != "https://github.example.com/api/v3" {
		t.Fatalf("client token/baseURL = %q / %q", client.token, client.baseURL)
	}
}

func TestNewGitHubClientUsesGitHubCLIFallback(t *testing.T) {
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	gh := filepath.Join(directory, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\nprintf 'cli-token\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_HOST", "")
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))

	client, err := newGitHubClient(context.Background())
	if err != nil {
		t.Fatalf("newGitHubClient() error = %v", err)
	}
	if client.token != "cli-token" || client.baseURL != "https://api.github.com" {
		t.Fatalf("client token/baseURL = %q / %q", client.token, client.baseURL)
	}
}

func TestCreateRepositoryUsesTemplateAndAppliesSettings(t *testing.T) {
	var requests []string
	settingsApplied := false
	server := httptest.NewServer(newRepositoryCreationHandler(t, &requests, &settingsApplied))
	defer server.Close()

	client := &gitHubClient{
		baseURL: server.URL,
		token:   "test-token",
		http:    server.Client(),
		signedRootCreator: func(context.Context, string, string, string) (string, string, error) {
			return "normalized-root", "initial-bootstrap", nil
		},
	}
	created, err := client.CreateRepository(context.Background(), createOptions{
		Name: "new-repo", Description: "Repository description", Visibility: "private",
	})
	if err != nil {
		t.Fatalf("CreateRepository() error = %v", err)
	}
	if created.HTMLURL != "https://github.com/upfera/new-repo" {
		t.Fatalf("created URL = %q", created.HTMLURL)
	}
	if len(requests) != 12 {
		t.Fatalf("number of requests = %d, want 12: %v", len(requests), requests)
	}
}

func newRepositoryCreationHandler(t *testing.T, requests *[]string, settingsApplied *bool) http.Handler {
	mux := http.NewServeMux()
	registerAPIRoute(t, mux, requests, "GET", "/repos/upfera/new-repo", repositorySettingsHandler(settingsApplied))
	registerAPIRoute(t, mux, requests, "POST", "/repos/upfera/.template/generate", repositoryGenerationHandler(t))
	registerAPIRoute(t, mux, requests, "GET", "/repos/upfera/new-repo/git/ref/heads/main", jsonHandler(http.StatusOK, map[string]any{"object": map[string]string{"sha": "template-root"}}))
	registerAPIRoute(t, mux, requests, "GET", "/repos/upfera/new-repo/git/commits/template-root", jsonHandler(http.StatusOK, map[string]any{"message": "Initial commit", "tree": map[string]string{"sha": "template-tree"}, "parents": []any{}}))
	registerAPIRoute(t, mux, requests, "GET", "/repos/upfera/new-repo/git/commits/normalized-root", jsonHandler(http.StatusOK, map[string]any{"message": initialCommitMessage, "tree": map[string]string{"sha": "template-tree"}, "parents": []any{}, "verification": map[string]bool{"verified": true}}))
	registerAPIRoute(t, mux, requests, "PATCH", "/repos/upfera/new-repo", repositoryUpdateHandler(t, settingsApplied))
	registerAPIRoute(t, mux, requests, "DELETE", "/repos/upfera/new-repo/git/refs/heads/{branch}", statusHandler(http.StatusNoContent))
	registerAPIRoute(t, mux, requests, "POST", "/repos/upfera/new-repo/git/refs", refCreationHandler(t))
	return mux
}

func registerAPIRoute(t *testing.T, mux *http.ServeMux, requests *[]string, method, path string, handler http.Handler) {
	mux.Handle(method+" "+path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.Path)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("Authorization header = %q", r.Header.Get("Authorization"))
		}
		handler.ServeHTTP(w, r)
	}))
}

func repositorySettingsHandler(settingsApplied *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !*settingsApplied {
			writeJSON(w, http.StatusNotFound, apiError{Message: "Not Found"})
			return
		}
		writeJSON(w, http.StatusOK, repository{
			Name:                     "new-repo",
			HTMLURL:                  "https://github.com/upfera/new-repo",
			AllowSquashMerge:         true,
			AllowMergeCommit:         false,
			AllowRebaseMerge:         false,
			SquashMergeCommitTitle:   squashTitle,
			SquashMergeCommitMessage: squashMessage,
		})
	})
}

func repositoryGenerationHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode generate request: %v", err)
		}
		if body["owner"] != "upfera" || body["name"] != "new-repo" || body["description"] != "Repository description" || body["private"] != true || body["include_all_branches"] != false {
			t.Errorf("generate request body = %#v", body)
		}
		writeJSON(w, http.StatusCreated, repository{Name: "new-repo", HTMLURL: "https://github.com/upfera/new-repo", DefaultBranch: "main"})
	})
}

func repositoryUpdateHandler(t *testing.T, settingsApplied *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode repository update: %v", err)
		}
		if branch, ok := body["default_branch"]; ok {
			if branch != "initial-bootstrap" && branch != "main" {
				t.Errorf("unexpected default branch %v", branch)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		assertJSONFields(t, body, map[string]any{
			"allow_squash_merge":          true,
			"allow_merge_commit":          false,
			"allow_rebase_merge":          false,
			"squash_merge_commit_title":   squashTitle,
			"squash_merge_commit_message": squashMessage,
		})
		*settingsApplied = true
		w.WriteHeader(http.StatusOK)
	})
}

func refCreationHandler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode ref creation: %v", err)
		}
		if body["ref"] != "refs/heads/initial-bootstrap" && body["ref"] != "refs/heads/main" {
			t.Errorf("unexpected ref creation: %#v", body)
		}
		if body["sha"] != "normalized-root" {
			t.Errorf("ref points to %q", body["sha"])
		}
		w.WriteHeader(http.StatusCreated)
	})
}

func assertJSONFields(t *testing.T, actual, expected map[string]any) {
	t.Helper()
	for key, value := range expected {
		if actual[key] != value {
			t.Errorf("field %s = %v, want %v", key, actual[key], value)
		}
	}
}

func jsonHandler(status int, value any) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, status, value)
	})
}

func statusHandler(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	})
}

func TestEnsureInitialCommitReportsGitHubTransitionFailures(t *testing.T) {
	failures := []struct {
		step string
		want string
	}{
		{step: "create-root", want: "create signed initial commit"},
		{step: "verify-root", want: "not a verified"},
		{step: "bootstrap-default", want: "switch default branch"},
		{step: "delete-source", want: "remove generated branch"},
		{step: "create-ref", want: "restore default branch"},
		{step: "restore-default", want: "restore repository default branch"},
		{step: "delete-bootstrap", want: "remove temporary bootstrap branch"},
	}
	for _, failure := range failures {
		t.Run(failure.step, func(t *testing.T) {
			server := httptest.NewServer(initialCommitFailureHandler(t, failure.step))
			defer server.Close()
			client := &gitHubClient{
				baseURL: server.URL,
				token:   "test-token",
				http:    server.Client(),
				signedRootCreator: func(context.Context, string, string, string) (string, string, error) {
					if failure.step == "create-root" {
						return "", "", fmt.Errorf("signing failed")
					}
					return "signed-root", "bootstrap", nil
				},
			}
			err := client.ensureInitialCommit(context.Background(), repository{Name: "sample", DefaultBranch: "main"})
			if err == nil || !strings.Contains(err.Error(), failure.want) {
				t.Fatalf("ensureInitialCommit() error = %v, want %q", err, failure.want)
			}
		})
	}
}

func initialCommitFailureHandler(t *testing.T, failure string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/upfera/sample/git/ref/heads/main", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"object": map[string]string{"sha": "template-root"}})
	})
	mux.HandleFunc("GET /repos/upfera/sample/git/commits/template-root", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"message": "template baseline", "tree": map[string]string{"sha": "template-tree"}, "parents": []any{}})
	})
	mux.HandleFunc("GET /repos/upfera/sample/git/commits/signed-root", func(w http.ResponseWriter, _ *http.Request) {
		verified := failure != "verify-root"
		writeJSON(w, http.StatusOK, map[string]any{"message": initialCommitMessage, "tree": map[string]string{"sha": "template-tree"}, "parents": []any{}, "verification": map[string]bool{"verified": verified}})
	})
	mux.HandleFunc("PATCH /repos/upfera/sample", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode branch update: %v", err)
		}
		if failure == "bootstrap-default" && body["default_branch"] == "bootstrap" || failure == "restore-default" && body["default_branch"] == "main" {
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "branch update failed"})
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("DELETE /repos/upfera/sample/git/refs/heads/{branch}", func(w http.ResponseWriter, r *http.Request) {
		branch := r.PathValue("branch")
		if failure == "delete-source" && branch == "main" || failure == "delete-bootstrap" && branch == "bootstrap" {
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "branch delete failed"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /repos/upfera/sample/git/refs", func(w http.ResponseWriter, _ *http.Request) {
		if failure == "create-ref" {
			writeJSON(w, http.StatusInternalServerError, apiError{Message: "ref creation failed"})
			return
		}
		w.WriteHeader(http.StatusCreated)
	})
	return mux
}

func TestCreateRepositoryChecksSigningBeforeCreation(t *testing.T) {
	var generateCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			generateCalled = true
		}
		if r.Method != http.MethodGet || r.URL.Path != "/repos/upfera/preflight" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	defer server.Close()

	client := &gitHubClient{
		baseURL: server.URL,
		token:   "test-token",
		http:    server.Client(),
		signerCheck: func(context.Context) error {
			return fmt.Errorf("configure a GitHub-verified signing key")
		},
	}
	_, err := client.CreateRepository(context.Background(), createOptions{Name: "preflight", Visibility: "private"})
	if err == nil || !strings.Contains(err.Error(), "signing key") {
		t.Fatalf("CreateRepository() error = %v, want signing key error", err)
	}
	if generateCalled {
		t.Fatal("repository generation started without a signing key")
	}
}

func TestCreateRepositoryDetectsExistingRepository(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/repos/upfera/already-there" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		writeJSON(w, http.StatusOK, repository{Name: "already-there"})
	}))
	defer server.Close()

	client := &gitHubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
	_, err := client.CreateRepository(context.Background(), createOptions{Name: "already-there", Visibility: "private"})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("CreateRepository() error = %v, want already exists", err)
	}
}

func TestCreateRepositoryReportsGitHubFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/upfera/failure":
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprint(w, `{"message":"Not Found"}`)
		case "/repos/upfera/.template/generate":
			w.WriteHeader(http.StatusForbidden)
			_, _ = fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &gitHubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
	_, err := client.CreateRepository(context.Background(), createOptions{Name: "failure", Visibility: "private"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 403: Resource not accessible by integration") {
		t.Fatalf("CreateRepository() error = %v, want clear GitHub API error", err)
	}
}

func TestCreateRepositoryVerifiesSettingsAndReportsMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "PATCH /repos/upfera/settings-repo":
			w.WriteHeader(http.StatusOK)
		case "GET /repos/upfera/settings-repo":
			writeJSON(w, http.StatusOK, repository{
				AllowSquashMerge:         true,
				AllowMergeCommit:         true,
				AllowRebaseMerge:         false,
				SquashMergeCommitTitle:   squashTitle,
				SquashMergeCommitMessage: squashMessage,
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := &gitHubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
	err := client.configureAndVerify(context.Background(), "settings-repo")
	if err == nil || !strings.Contains(err.Error(), "unexpected merge settings") {
		t.Fatalf("configureAndVerify() error = %v, want settings mismatch", err)
	}
}

func TestGitHubRequestReportsEncodingNetworkAndResponseFailures(t *testing.T) {
	client := &gitHubClient{baseURL: "https://api.github.com", token: "test-token", http: http.DefaultClient}
	if err := client.request(context.Background(), http.MethodPost, "/repos", func() {}, nil); err == nil || !strings.Contains(err.Error(), "encode GitHub request") {
		t.Fatalf("request() encoding error = %v", err)
	}
	client.baseURL = "://invalid"
	if err := client.request(context.Background(), http.MethodGet, "/repos", nil, nil); err == nil || !strings.Contains(err.Error(), "create GitHub request") {
		t.Fatalf("request() URL error = %v", err)
	}

	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := closed.URL
	closed.Close()
	client.baseURL = closedURL
	if err := client.request(context.Background(), http.MethodGet, "/repos", nil, nil); err == nil || !strings.Contains(err.Error(), "contact GitHub API") {
		t.Fatalf("request() network error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not JSON")
	}))
	defer server.Close()
	client.baseURL = server.URL
	if err := client.request(context.Background(), http.MethodGet, "/repos", nil, &repository{}); err == nil || !strings.Contains(err.Error(), "decode GitHub response") {
		t.Fatalf("request() decode error = %v", err)
	}
}

func TestGitHubRequestUsesStatusTextForInvalidErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = fmt.Fprint(w, "not JSON")
	}))
	defer server.Close()
	client := &gitHubClient{baseURL: server.URL, token: "test-token", http: server.Client()}
	err := client.request(context.Background(), http.MethodGet, "/repos", nil, nil)
	apiErr, ok := err.(*githubError)
	if !ok || apiErr.StatusCode != http.StatusTeapot || apiErr.Message != http.StatusText(http.StatusTeapot) {
		t.Fatalf("request() error = %#v, want HTTP 418 status text", err)
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}
