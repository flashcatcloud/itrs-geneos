# ITRS Geneos to FlashDuty

[![CI](https://github.com/flashcatcloud/itrs-geneos/actions/workflows/ci.yml/badge.svg)](https://github.com/flashcatcloud/itrs-geneos/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/flashcatcloud/itrs-geneos)](https://github.com/flashcatcloud/itrs-geneos/releases)

English | [简体中文](README.zh-CN.md)

`geneos-flashduty` is a small, standalone Go executable that converts ITRS Geneos Alerting Effect or Rule Action events into [FlashDuty standard alert events](https://flashcat.cloud/product/flashduty/). It runs once per event, keeps no local state, and can be deployed as a single binary on the Geneos Gateway host.

## How it works

```text
Geneos Alerting + Effect --\
                            +--> geneos-flashduty --> FlashDuty Standard Alert API
Geneos Rule + Action -------/
```

Trigger and recovery events are correlated by a stable `alert_key`:

```text
_VARIABLEPATH is present
  geneos:v1:<SHA-256(_VARIABLEPATH)>

_VARIABLEPATH is absent and stable Geneos fields are sufficient
  geneos:v1:fallback:<SHA-256(canonical-fields)>

No stable identity is available
  geneos:v1:random:<UUID v4>
```

The random fallback still delivers the event, but a later recovery cannot be guaranteed to match it. When `_VARIABLEPATH` is present, its original value is also sent as the `geneos_variable_path` label for diagnostics. The full value is always used for hashing even if the label must be truncated to meet protocol limits.

## Status mapping

| Geneos context | FlashDuty `event_status` |
| --- | --- |
| `_ALERT_TYPE=clear` | `Ok` |
| `_SEVERITY=OK` | `Ok` |
| `_SEVERITY=CRITICAL` | `Critical` |
| `_SEVERITY=WARNING` | `Warning` |
| `_SEVERITY=INFO` or `UNDEFINED` | `Info` |

The `resolve` command always sends `Ok`. FlashDuty's standard event protocol has no equivalent of PagerDuty's `acknowledge`, so Geneos suspend and assignment events are not mapped to acknowledgement. Ownership, on-call, and escalation stay in FlashDuty.

## Install

Download the binary for your Gateway host from [GitHub Releases](https://github.com/flashcatcloud/itrs-geneos/releases), verify it against `SHA256SUMS`, and install it in a path available to Geneos. For example:

```bash
install -m 0755 geneos-flashduty-v1.0.0-linux-amd64 \
  /opt/itrs/gateway/gateway_shared/geneos-flashduty
```

To build from source, use Go 1.22 or newer:

```bash
go test ./...
go build -o geneos-flashduty ./cmd/geneos-flashduty
```

## Configure

Copy [`flashduty.example.yaml`](flashduty.example.yaml) to one of these locations:

1. the path passed with `--config`;
2. `./flashduty.yaml`;
3. `$HOME/.config/geneos/flashduty.yaml`;
4. `/etc/geneos/flashduty.yaml`.

The first matching file wins. An explicit but unreadable `--config` path is an error; missing implicit files are ignored.

Minimal configuration:

```yaml
flashduty:
  endpoint: https://api.flashcat.cloud/event/push/alert/standard
  integration_key: your-integration-key
```

`flashduty.endpoint` can be changed if your FlashDuty push URL changes. If it is omitted, the URL above is the compiled default. The program appends the integration key as the `integration_key` query parameter, including when the endpoint already has other query parameters.

For better secret handling and per-entity routing, set the key as a Geneos Managed Entity or Managed Entity Group attribute instead:

```text
FLASHDUTY_INTEGRATION_KEY=<your-integration-key>
```

This environment value overrides the YAML value. Trigger and recovery events must use the same integration key; otherwise they reach different FlashDuty integrations and cannot correlate even when their `alert_key` values match.

The example configuration also documents timeout, retry, severity, title, description, and custom-label settings. `${NAME}` references in title, description, and custom label values expand from environment variables. Endpoint, key, timeout, retries, key prefix, and severity mapping are not template-expanded.

## Configure Geneos

Use [`examples/geneos-effect.xml`](examples/geneos-effect.xml) as the recommended Alerting Effect example, or [`examples/geneos-action.xml`](examples/geneos-action.xml) for a Rule Action.

An Alerting Effect receives Geneos lifecycle events, including `_ALERT_TYPE=clear`, and therefore maps recovery automatically. When using a Rule Action, configure the rule so that the action also runs when the severity returns to `OK`.

The executable recognizes a strict allow-list of Geneos context variables instead of sending the entire process environment. Common labels include Gateway, Probe, Managed Entity, Sampler, Dataview, row, column, rule, Geneos severity, alert type, and variable path.

## Commands

Automatic Effect or Action mode:

```bash
geneos-flashduty
```

Force a trigger or recovery with a known variable path:

```bash
geneos-flashduty trigger --variable-path '/geneos/.../cell'
geneos-flashduty resolve --variable-path '/geneos/.../cell'
```

Validate configuration and test connectivity:

```bash
geneos-flashduty test --config /etc/geneos/flashduty.yaml
```

Test mode sends `Info` and then `Ok` with the same generated key so it does not intentionally leave an open alert.

## Delivery behavior

- Per-attempt timeout defaults to 10 seconds.
- The initial request is followed by up to three retries by default.
- Network errors, HTTP 429, and HTTP 5xx are retried with exponential backoff and jitter.
- `Retry-After` is honored up to 60 seconds.
- Other HTTP 4xx responses are not retried.
- Success exits with code 0; validation or final delivery failure exits non-zero.
- The integration key is redacted from logs.
- Unknown environment variables are not included in the payload.

## Troubleshooting

- `FlashDuty integration key is required`: set `FLASHDUTY_INTEGRATION_KEY` or `flashduty.integration_key`.
- HTTP 401/403: verify that the endpoint and key belong to the same FlashDuty integration.
- An alert does not recover: verify the same `_VARIABLEPATH`, key algorithm version, and integration key are used for both events.
- `alert_key_source=random` appears in logs: Geneos did not provide enough stable identity data, so recovery correlation cannot be guaranteed.
- Connection failure: confirm that the Gateway host can reach the configured endpoint over HTTPS.

## Development

```bash
gofmt -w .
go test -race ./...
go vet ./...
go build ./cmd/geneos-flashduty
```

Version tags matching `v*` build Linux and macOS executables for AMD64 and ARM64, publish `SHA256SUMS`, and create a GitHub Release.

## License

[MIT](LICENSE)
