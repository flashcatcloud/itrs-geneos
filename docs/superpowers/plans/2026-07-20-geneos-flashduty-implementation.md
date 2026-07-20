# Geneos to FlashDuty Implementation Plan

1. Create the Go module and configuration package, including compiled defaults, YAML overlay, file discovery, template expansion, duration validation, retry validation, and integration-key environment override tests.
2. Create the Geneos context and event packages, with tests for status precedence, deterministic `_VARIABLEPATH` hashing, stable-component fallback, random fallback, labels, truncation, and trigger/recovery correlation.
3. Create the FlashDuty HTTP client, with `httptest` coverage for payload delivery, request IDs, secret redaction, timeout handling, retryable statuses, `Retry-After`, and non-retryable 4xx responses.
4. Create the CLI entrypoint for automatic, trigger, resolve, and test modes; add CLI-oriented unit tests for argument parsing and event construction.
5. Add example YAML, Geneos Effect XML, Geneos Action XML, and a README covering installation, identity behavior, recovery requirements, security, and troubleshooting.
6. Run formatting, `go test ./...`, `go vet ./...`, native build, and Linux AMD64/ARM64 cross-builds; fix all failures and commit the completed implementation.
