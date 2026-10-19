# gh-upfera

`gh-upfera` is a GitHub CLI extension for creating repositories in the `upfera` organization from [`upfera/.template`](https://github.com/upfera/.template). It applies the organization’s squash-merge defaults and creates a single, signed initial commit.

## Install

```sh
gh extension install upfera/gh-upfera
```

The GitHub CLI and Git must be installed. Authenticate with `gh auth login` or set `GH_TOKEN`/`GITHUB_TOKEN`. Repository creation also requires a Git signing key that GitHub recognizes as verified, plus a configured Git author name and email. The extension uses the normal Git signing configuration; it does not change your identity or signing settings.

The authenticated account must be allowed to create repositories in `upfera`, read the template, update repository settings, and push repository contents. For fine-grained tokens, grant the required organization repository-creation permission and repository `Contents` and `Administration` write access.

## Usage

```sh
gh upfera repo create <name> [--description <text>] [--visibility public|private]
```

Repositories are private by default. Use `--public` or `--private` as a shortcut for `--visibility`. Names must be 1–100 ASCII letters, digits, periods, underscores, or hyphens; descriptions can be up to 350 characters.

The command checks that the name is available before creating the repository. GitHub may still reject the operation if the name is taken concurrently or organization policy denies it. If a later setup step fails, the command reports that the repository was created and identifies the failed step.

## Development

```sh
go test ./...
go vet ./...
go build ./...
```

Tests use an in-process HTTP server and temporary local Git repositories. They do not create GitHub repositories or require credentials.

GitHub Actions runs tests, `go vet`, and a build on every branch push and pull request. SonarCloud analysis runs as the `SonarCloud Code Analysis` status check with the organization-level `SONAR_TOKEN` secret. Branch protection requires that status check and verified commit signatures.
