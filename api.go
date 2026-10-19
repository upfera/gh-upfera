package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	githubReposPath      = "/repos/"
	upferaOwner          = "upfera"
	templateRepository   = "upfera/.template"
	squashTitle          = "PR_TITLE"
	squashMessage        = "PR_BODY"
	initialCommitMessage = "chore: initial commit"
)

type repository struct {
	Name                     string `json:"name"`
	HTMLURL                  string `json:"html_url"`
	CloneURL                 string `json:"clone_url"`
	DefaultBranch            string `json:"default_branch"`
	AllowSquashMerge         bool   `json:"allow_squash_merge"`
	AllowMergeCommit         bool   `json:"allow_merge_commit"`
	AllowRebaseMerge         bool   `json:"allow_rebase_merge"`
	SquashMergeCommitTitle   string `json:"squash_merge_commit_title"`
	SquashMergeCommitMessage string `json:"squash_merge_commit_message"`
}

type apiError struct {
	Message string `json:"message"`
}

type githubError struct {
	StatusCode int
	Message    string
}

func (e *githubError) Error() string {
	return fmt.Sprintf("GitHub API returned HTTP %d: %s", e.StatusCode, e.Message)
}

type gitHubClient struct {
	baseURL           string
	token             string
	http              *http.Client
	signerCheck       func(context.Context) error
	signedRootCreator func(context.Context, string, string, string) (string, string, error)
}

func newGitHubClient(ctx context.Context) (*gitHubClient, error) {
	token := os.Getenv("GH_TOKEN")
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		command, err := commandContext(ctx, "gh", "auth", "token")
		if err != nil {
			return nil, fmt.Errorf("no GitHub token found; run `gh auth login` or set GH_TOKEN")
		}
		output, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("no GitHub token found; run `gh auth login` or set GH_TOKEN")
		}
		token = strings.TrimSpace(string(output))
	}
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found; run `gh auth login` or set GH_TOKEN")
	}

	baseURL := "https://api.github.com"
	if host := os.Getenv("GH_HOST"); host != "" && host != "github.com" {
		baseURL = "https://" + host + "/api/v3"
	}
	return &gitHubClient{
		baseURL:     baseURL,
		token:       token,
		http:        &http.Client{Timeout: 30 * time.Second},
		signerCheck: checkSigningKey,
	}, nil
}

func (c *gitHubClient) request(ctx context.Context, method, path string, body any, result any) error {
	var requestBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode GitHub request: %w", err)
		}
		requestBody = bytes.NewReader(data)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, requestBody)
	if err != nil {
		return fmt.Errorf("create GitHub request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("contact GitHub API: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var apiErr apiError
		_ = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&apiErr)
		if apiErr.Message == "" {
			apiErr.Message = http.StatusText(response.StatusCode)
		}
		return &githubError{StatusCode: response.StatusCode, Message: apiErr.Message}
	}
	if result == nil || response.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(result); err != nil {
		return fmt.Errorf("decode GitHub response: %w", err)
	}
	return nil
}

func (c *gitHubClient) CreateRepository(ctx context.Context, options createOptions) (repository, error) {
	if err := validateCreateOptions(options); err != nil {
		return repository{}, err
	}

	name := url.PathEscape(options.Name)
	var existing repository
	err := c.request(ctx, http.MethodGet, githubReposPath+upferaOwner+"/"+name, nil, &existing)
	if err == nil {
		return repository{}, fmt.Errorf("repository %s/%s already exists", upferaOwner, options.Name)
	}
	var apiErr *githubError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusNotFound {
		return repository{}, fmt.Errorf("check repository availability: %w", err)
	}
	if c.signerCheck != nil {
		if err := c.signerCheck(ctx); err != nil {
			return repository{}, err
		}
	}

	var created repository
	payload := map[string]any{
		"owner":                upferaOwner,
		"name":                 options.Name,
		"description":          options.Description,
		"private":              options.Visibility == "private",
		"include_all_branches": false,
	}
	if err := c.request(ctx, http.MethodPost, githubReposPath+templateRepository+"/generate", payload, &created); err != nil {
		return repository{}, fmt.Errorf("create repository from %s: %w", templateRepository, err)
	}

	if err := c.ensureInitialCommit(ctx, created); err != nil {
		return created, fmt.Errorf("repository %s was created, but its initial commit could not be normalized to %q: %w", created.HTMLURL, initialCommitMessage, err)
	}
	if err := c.configureAndVerify(ctx, options.Name); err != nil {
		return created, fmt.Errorf("repository %s was created, but settings could not be applied and verified: %w", created.HTMLURL, err)
	}
	return created, nil
}

type gitRef struct {
	Object struct {
		SHA string `json:"sha"`
	} `json:"object"`
}

type gitCommit struct {
	Message string `json:"message"`
	Tree    struct {
		SHA string `json:"sha"`
	} `json:"tree"`
	Parents      []struct{} `json:"parents"`
	Verification struct {
		Verified bool `json:"verified"`
	} `json:"verification"`
}

func (c *gitHubClient) ensureInitialCommit(ctx context.Context, repo repository) error {
	branch := repo.DefaultBranch
	if branch == "" {
		branch = "main"
	}
	branchPath := url.PathEscape(branch)
	var ref gitRef
	if err := c.request(ctx, http.MethodGet, githubReposPath+upferaOwner+"/"+url.PathEscape(repo.Name)+"/git/ref/heads/"+branchPath, nil, &ref); err != nil {
		return fmt.Errorf("read generated default branch: %w", err)
	}

	commitPath := githubReposPath + upferaOwner + "/" + url.PathEscape(repo.Name) + "/git/commits/" + url.PathEscape(ref.Object.SHA)
	var oldCommit gitCommit
	if err := c.request(ctx, http.MethodGet, commitPath, nil, &oldCommit); err != nil {
		return fmt.Errorf("read generated initial commit: %w", err)
	}
	if oldCommit.Message == initialCommitMessage && len(oldCommit.Parents) == 0 && oldCommit.Verification.Verified {
		return nil
	}

	createRoot := c.signedRootCreator
	if createRoot == nil {
		createRoot = c.createSignedRootCommit
	}
	rootSHA, bootstrapBranch, err := createRoot(ctx, repo.Name, branch, oldCommit.Tree.SHA)
	if err != nil {
		return fmt.Errorf("create signed initial commit: %w", err)
	}
	var signedRoot gitCommit
	rootCommitPath := githubReposPath + upferaOwner + "/" + url.PathEscape(repo.Name) + "/git/commits/" + url.PathEscape(rootSHA)
	if err := c.request(ctx, http.MethodGet, rootCommitPath, nil, &signedRoot); err != nil {
		return fmt.Errorf("verify signed initial commit: %w", err)
	}
	if signedRoot.Message != initialCommitMessage || len(signedRoot.Parents) != 0 || !signedRoot.Verification.Verified || signedRoot.Tree.SHA != oldCommit.Tree.SHA {
		return fmt.Errorf("new initial commit is not a verified, parentless %q commit with the template tree", initialCommitMessage)
	}
	if err := c.setDefaultBranch(ctx, repo.Name, bootstrapBranch); err != nil {
		return fmt.Errorf("switch default branch to temporary bootstrap: %w", err)
	}
	if err := c.deleteRef(ctx, repo.Name, branch); err != nil {
		return fmt.Errorf("remove generated branch before replacing its root commit: %w", err)
	}
	if err := c.createRef(ctx, repo.Name, branch, rootSHA); err != nil {
		return fmt.Errorf("restore default branch with normalized initial commit: %w", err)
	}
	if err := c.setDefaultBranch(ctx, repo.Name, branch); err != nil {
		return fmt.Errorf("restore repository default branch: %w", err)
	}
	if err := c.deleteRef(ctx, repo.Name, bootstrapBranch); err != nil {
		return fmt.Errorf("remove temporary bootstrap branch: %w", err)
	}
	return nil
}

func (c *gitHubClient) createRef(ctx context.Context, repo, branch, sha string) error {
	body := map[string]string{"ref": "refs/heads/" + branch, "sha": sha}
	return c.request(ctx, http.MethodPost, githubReposPath+upferaOwner+"/"+url.PathEscape(repo)+"/git/refs", body, nil)
}

func (c *gitHubClient) deleteRef(ctx context.Context, repo, branch string) error {
	path := githubReposPath + upferaOwner + "/" + url.PathEscape(repo) + "/git/refs/heads/" + url.PathEscape(branch)
	return c.request(ctx, http.MethodDelete, path, nil, nil)
}

func (c *gitHubClient) setDefaultBranch(ctx context.Context, repo, branch string) error {
	body := map[string]string{"default_branch": branch}
	return c.request(ctx, http.MethodPatch, githubReposPath+upferaOwner+"/"+url.PathEscape(repo), body, nil)
}

func (c *gitHubClient) configureAndVerify(ctx context.Context, name string) error {
	path := githubReposPath + upferaOwner + "/" + url.PathEscape(name)
	settings := map[string]any{
		"allow_squash_merge":          true,
		"allow_merge_commit":          false,
		"allow_rebase_merge":          false,
		"squash_merge_commit_title":   squashTitle,
		"squash_merge_commit_message": squashMessage,
	}
	if err := c.request(ctx, http.MethodPatch, path, settings, nil); err != nil {
		return fmt.Errorf("apply squash-merge settings: %w", err)
	}

	var actual repository
	if err := c.request(ctx, http.MethodGet, path, nil, &actual); err != nil {
		return fmt.Errorf("read back repository settings: %w", err)
	}
	if !actual.AllowSquashMerge || actual.AllowMergeCommit || actual.AllowRebaseMerge || actual.SquashMergeCommitTitle != squashTitle || actual.SquashMergeCommitMessage != squashMessage {
		return fmt.Errorf("GitHub returned unexpected merge settings (squash=%t, merge=%t, rebase=%t, title=%q, message=%q)", actual.AllowSquashMerge, actual.AllowMergeCommit, actual.AllowRebaseMerge, actual.SquashMergeCommitTitle, actual.SquashMergeCommitMessage)
	}
	return nil
}
