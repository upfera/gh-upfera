# AGENTS.md

Instructions for AI coding agents and automation working in this repository. Humans: see the
organization's [CONTRIBUTING.md](https://github.com/upfera/.github/blob/main/CONTRIBUTING.md).

## Workflow

- Work from an issue. Branch from `main`; never commit directly to `main`.
- One focused change per pull request, linked with `Closes #N`.
- Pull request titles and commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).
- Pull requests are squash-merged; the PR title becomes the commit message.

## Commits

- Commits are signed and authored by the configured git identity. Do not change `user.name` or `user.email`.
- Do not add `Co-authored-by` or `Signed-off-by` trailers, or any other attribution lines.

## Quality

- Run the project's build, tests, and linters before opening a pull request, and say what you ran in the PR.
- Update documentation together with the code it describes.
- Never commit secrets or credentials. Local configuration belongs in `.env` (ignored); document variables in `.env.example`.

## Project commands

- Build: `go build ./...`
- Test: `go test ./...`
- Lint: `go vet ./...`
- Format: `gofmt -w *.go`

## More context

Longer agent-facing material (specs, decisions, prompts, skills) lives in [`.ai/`](.ai/).
