<!-- template-source: danstis/base-template -->
# CLAUDE.md

This file provides guidance for Claude Code when working in this repository.

## What this repo is

This is a GitHub template repository. Its purpose is to be used as the starting point for new repositories, not as a working project itself. When a new repo is created from this template, a one-shot GitHub Actions workflow runs to personalise the files and then removes itself.

## Repository structure

```
.github/workflows/template-init.yml   # One-shot initialisation workflow (self-destructs on first run)
.gitattributes                         # LF line-ending normalisation for all text files
CODE_OF_CONDUCT.md                     # Community standards
CONTRIBUTING.md                        # Contribution guidelines
LICENSE                                # MIT licence with {{ YEAR }} placeholder
README.md                              # Repo readme with {{ REPO_NAME }} and {{ REPO_DESCRIPTION }} placeholders
SECURITY.md                            # Vulnerability reporting policy
SUPPORT.md                             # How to get help
```

## Placeholders

Files in this template use double-brace placeholders that are substituted during initialisation:

| Placeholder | Resolved from |
|---|---|
| `{{ REPO_NAME }}` | Git remote URL (basename) |
| `{{ REPO_DESCRIPTION }}` | GitHub repo description (`gh repo view`) |
| `{{ YEAR }}` | Current year at initialisation time |

## Initialisation workflow

`.github/workflows/template-init.yml` runs once on the first push to `main` of a new repo (skipped if the repo name is `base-template` itself). It:

1. Substitutes all placeholders in `LICENSE` and `README.md`.
2. Removes itself (`git rm .github/workflows/template-init.yml`).
3. Commits and pushes the result.

After that, the workflow no longer exists in the consuming repo.

## Key constraints when editing this template

- **Keep all content generic.** No file should reference a specific project name, technology stack, or tooling (e.g. Docker, Dev Containers, VS Code). Content must apply equally to any repository created from this template.
- **Do not add project-specific logic to the workflow.** The init workflow should remain minimal — placeholder substitution and self-removal only.
- **Preserve all placeholders.** Do not resolve or remove `{{ REPO_NAME }}`, `{{ REPO_DESCRIPTION }}`, or `{{ YEAR }}` — they are substituted at initialisation time.
- **LINE ENDINGS:** All text files must use LF (enforced by `.gitattributes`). Do not introduce CRLF.
