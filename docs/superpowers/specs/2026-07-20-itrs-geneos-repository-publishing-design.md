# ITRS Geneos Repository Publishing Design

Date: 2026-07-20

## Objective

Publish the completed Geneos-to-FlashDuty adapter to `flashcatcloud/itrs-geneos` as a conventional, maintainable Go project. Preserve the target repository's existing `main` history and MIT license, present English documentation by default with a complete Chinese translation, and distribute executables through tagged GitHub Releases instead of committing generated binaries.

## Publishing Strategy

The target repository is the source of truth. Clone its existing `main` branch, create `agent/initial-geneos-flashduty-integration`, and import the task-scoped files from the local implementation. Do not force-push or replace the target repository history.

After validation, push the feature branch and open a draft pull request into `main`. The pull request will describe the integration, alert lifecycle behavior, documentation, and checks run. The existing target `LICENSE` remains unchanged.

## Repository Layout

```text
itrs-geneos/
├── .github/
│   └── workflows/
│       ├── ci.yml
│       └── release.yml
├── cmd/
│   └── geneos-flashduty/
│       └── main.go
├── docs/
│   └── superpowers/
│       ├── plans/
│       └── specs/
├── examples/
│   ├── geneos-action.xml
│   └── geneos-effect.xml
├── internal/
│   ├── app/
│   ├── config/
│   ├── event/
│   ├── flashduty/
│   └── geneos/
├── .gitignore
├── LICENSE
├── README.md
├── README.zh-CN.md
├── flashduty.example.yaml
├── go.mod
└── go.sum
```

The repository remains a single-purpose Go project. The executable keeps the name `geneos-flashduty`, while the Go module path changes from `github.com/flashcatcloud/geneos-flashduty` to `github.com/flashcatcloud/itrs-geneos` so imports match the published repository.

Generated files are excluded with `.gitignore`, including `dist/`, platform binaries, coverage output, editor files, and `.DS_Store`.

## Documentation

`README.md` is the default English entry point and links to `README.zh-CN.md`. The Chinese README links back to English. Both documents cover:

- the Geneos Effect and Rule Action execution models;
- installation from a release binary and building from source;
- YAML configuration and configuration-file discovery;
- the configurable FlashDuty endpoint and `integration_key` query parameter;
- `_VARIABLEPATH` hashing, stable-field fallback, and random UUID fallback;
- trigger/recovery correlation requirements;
- the `geneos_variable_path` diagnostic label;
- Geneos XML examples, CLI modes, retries, security, and troubleshooting.

The endpoint is documented as configurable through `flashduty.endpoint`, with `https://api.flashcat.cloud/event/push/alert/standard` as the compiled default. The integration key remains configurable through YAML or `FLASHDUTY_INTEGRATION_KEY`; endpoint environment-variable support is not added because YAML already covers the stated need.

## Continuous Integration

`.github/workflows/ci.yml` runs on pull requests and pushes to `main`. It uses the Go version declared by the module and performs:

1. `gofmt` verification;
2. `go test -race ./...`;
3. `go vet ./...`;
4. a normal `go build ./cmd/geneos-flashduty`.

Dependencies are cached through the official Go setup action. The workflow receives read-only repository permissions.

## Releases

`.github/workflows/release.yml` runs only for version tags matching `v*`. It builds static executables with `CGO_ENABLED=0` for:

- Linux AMD64;
- Linux ARM64;
- macOS AMD64;
- macOS ARM64.

Each artifact is named with the project, version, operating system, and architecture. The workflow generates `SHA256SUMS` and attaches the binaries and checksum file to a GitHub Release. Release creation uses only the repository content write permission required for that job.

No binary under the local `dist/` directory is committed. This prevents stale executables and keeps releases reproducible from tags.

## Functional Behavior Preserved

Repository restructuring does not change adapter behavior. In particular:

- `_VARIABLEPATH` produces a deterministic SHA-256-based `alert_key`;
- stable Geneos identity fields are used when `_VARIABLEPATH` is absent;
- a cryptographically random UUID is used only when no stable identity is available;
- the original `_VARIABLEPATH` is sent as `geneos_variable_path` when present;
- trigger and recovery events correlate only when they use the same identity and FlashDuty integration;
- `flashduty.endpoint` can override the compiled push URL;
- the integration key is appended as the `integration_key` query parameter and redacted from logs.

## Validation and Acceptance

Before publishing, run:

```text
gofmt verification
go test ./...
go test -race ./...
go vet ./...
native build
Linux AMD64 build
Linux ARM64 build
```

Also inspect the final diff to confirm that no integration key, generated binary, `.DS_Store`, or unrelated file is included. Publishing is complete when the branch is pushed to `flashcatcloud/itrs-geneos` and a draft pull request targeting `main` is available for review.
