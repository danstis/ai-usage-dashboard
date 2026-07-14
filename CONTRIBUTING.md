# Contributing

Thanks for contributing to this project.

## Basic workflow

1. Fork or branch from the default branch.
2. Make focused changes with clear commit messages.
3. Update documentation for any behavior/config changes.
4. Run relevant checks locally (`make build`, `make test`, `make lint`).
5. Open a PR with context, testing notes, and trade-offs.

## Contribution guidelines

- Keep changes focused and well-scoped.
- Document any new configuration, environment variables, or prerequisites
  in `README.md` (Configuration and Runtime sections).
- Explain version/dependency decisions where relevant.
- Use [Conventional Commits](https://www.conventionalcommits.org/) for
  every PR title and squash commit (see "Releases" below). release-please
  uses these prefixes to decide version bumps and changelog entries.
- Merge pull requests using **"Squash and merge"**. The repo allows merge,
  rebase, and squash; squash is the only option that keeps `main` linear
  and avoids duplicate entries in the release-please `CHANGELOG.md` (a
  merge commit and the underlying conventional commit would otherwise
  both be parsed and produce the same changelog entry).

## Suggested PR checklist

- [ ] Change is scoped and documented.
- [ ] README reflects user-visible behavior (Configuration, Runtime,
      Logging, HTTP API sections as relevant).
- [ ] `make build`, `make test`, and `make lint` pass locally.
- [ ] Validation steps were run (or limitations are noted).
- [ ] Licensing and security impacts were considered.

## Dependency updates (Renovate)

Renovate manages `gomod`, `github-actions`, and `dockerfile` updates
(see `renovate.json`). PRs are auto-labelled by manager:

| Manager          | Labels                              |
| ---------------- | ----------------------------------- |
| `gomod`          | `dependencies`, `deps:go-modules`   |
| `github-actions` | `dependencies`, `deps:github-actions` |
| `dockerfile`     | `dependencies`, `deps:docker`       |

GitHub Actions are pinned to commit digests; bumping an action means
merging a Renovate PR that updates the digest (the major/minor tag is
preserved as a comment). The Dependency Dashboard issue (opened by
Renovate on this repo) lists every pending or rate-limited update — check
it before opening manual update PRs to avoid duplicates.

## Releases

release-please watches `main` for Conventional Commit messages and opens
a "Release PR" that bumps the version, regenerates `CHANGELOG.md`, and
produces the release commit. Merging the Release PR publishes the GitHub
release and pushes a `vX.Y.Z` tag, which the `publish.yml` workflow uses
to publish the container image to GHCR with a semver tag.

Conventional Commit prefixes and their effect:

| Prefix      | Version bump | Changelog section |
| ----------- | ------------ | ----------------- |
| `feat:`     | minor        | Features          |
| `fix:`      | patch        | Bug Fixes         |
| `perf:`     | patch        | Performance       |
| `feat!:` / `BREAKING CHANGE:` | major | ⚠ BREAKING CHANGES |
| `docs:`, `chore:`, `refactor:`, `test:`, `ci:`, `build:`, `style:` | none | Miscellaneous / appropriate section |

## Development principles

- **Clarity:** future maintainers should understand what changed and why.
- **Simplicity:** prefer straightforward solutions over clever ones.
- **Correctness:** validate your changes before opening a PR.

## Code of conduct

By participating in this project, you agree to follow the [Code of Conduct](./CODE_OF_CONDUCT.md).
