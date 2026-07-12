<!-- template-source: danstis/base-template -->
# AGENTS.md

This file provides guidance for AI coding agents (Copilot, Cursor, etc.) working in this repository.

## What this repo is

This is a GitHub template repository used as the starting point for new repositories. It is not a working project. On first use, an Actions workflow personalises the files and removes itself.

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

Files use double-brace placeholders substituted during initialisation:

| Placeholder | Resolved from |
|---|---|
| `{{ REPO_NAME }}` | Git remote URL (basename) |
| `{{ REPO_DESCRIPTION }}` | GitHub repo description (`gh repo view`) |
| `{{ YEAR }}` | Current year at initialisation time |

## Initialisation workflow

`.github/workflows/template-init.yml` triggers on the first push to `main` in a new repo (skipped when the repo name is `base-template`). It:

1. Resolves `{{ YEAR }}`, `{{ REPO_NAME }}`, and `{{ REPO_DESCRIPTION }}` in `LICENSE` and `README.md`.
2. Removes itself from the repository.
3. Commits and pushes the changes.

## Rules for agents editing this template

- **All content must remain generic.** Nothing should reference a specific project, technology, or tool. Every file must be equally valid for any new repository created from this template.
- **Do not resolve placeholders.** Leave `{{ REPO_NAME }}`, `{{ REPO_DESCRIPTION }}`, and `{{ YEAR }}` as-is — they are substituted at runtime by the workflow.
- **Do not expand the init workflow scope.** It should only substitute placeholders and remove itself. Do not add project-specific steps.
- **If you add a new placeholder to any file, add the corresponding substitution line to the workflow.**
- **Line endings must be LF.** Enforced by `.gitattributes` — do not introduce CRLF.
