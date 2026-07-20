# Geneos to FlashDuty Adapter Design

Date: 2026-07-20

## Objective

Build a standalone Go executable named `geneos-flashduty` that converts ITRS Geneos alert context into FlashDuty standard alert events. The adapter must preserve the lifecycle relationship between trigger and recovery events, work with both Geneos Alerting Effects and Rule Actions, require no local state, and be deployable as a single Linux binary.

## Scope

The adapter will:

- read Geneos event context from environment variables;
- load defaults and optional YAML configuration;
- accept the FlashDuty integration key from configuration, with a Geneos environment-variable override;
- derive a deterministic FlashDuty `alert_key` from the Geneos data-item identity;
- retain the original Geneos `_VARIABLEPATH` as a FlashDuty label;
- map Geneos severity and alert lifecycle information to FlashDuty event states;
- POST events to the FlashDuty standard alert endpoint;
- retry transient failures and return a meaningful process exit code;
- provide automatic, trigger, resolve, and test command modes;
- include example Geneos Effect and Action XML.

The adapter will not:

- run as a daemon or HTTP service;
- persist event state locally;
- synchronize FlashDuty incidents back into Geneos;
- map Geneos assignment or snooze state to FlashDuty acknowledgement, because the FlashDuty standard alert event protocol has no equivalent acknowledgement event;
- send every Geneos environment variable by default.

## Architecture

Each Geneos event starts a short-lived process:

```text
Geneos Alerting + Effect --\
                            +--> geneos-flashduty --> HTTPS POST --> FlashDuty
Geneos Rule + Action -------/
```

The process has five internal responsibilities:

1. `config`: locate and load YAML, apply defaults, expand environment references, and apply direct environment overrides.
2. `geneos`: collect known Geneos environment variables and expose a validated event context.
3. `event`: select event status, derive `alert_key`, render text fields, and construct the FlashDuty payload.
4. `flashduty`: send the request, parse the response, retry transient failures, and redact secrets from logs.
5. `cmd`: implement automatic, `trigger`, `resolve`, and `test` modes.

No runtime state is necessary because trigger and recovery events independently derive the same key from stable Geneos identity fields.

## Command Interface

```text
geneos-flashduty [--config PATH]
geneos-flashduty trigger [--config PATH] [--variable-path PATH]
geneos-flashduty resolve [--config PATH] [--variable-path PATH]
geneos-flashduty test [--config PATH]
```

- No subcommand means automatic mode for Geneos Effect or Action execution.
- `trigger` forces an active event but uses the mapped Geneos severity when it is Critical, Warning, or Info; if no usable severity exists, it uses Warning.
- `resolve` forces `event_status=Ok`.
- `--variable-path` overrides `_VARIABLEPATH` for explicit trigger and resolve operations.
- `test` validates configuration, sends an Info test event, and then sends an Ok event with the same generated test key so it does not leave an intentionally open test alert.

## Configuration

Configuration is resolved in this order, with the first file found winning:

1. the path supplied by `--config`;
2. `./flashduty.yaml`;
3. `$HOME/.config/geneos/flashduty.yaml`;
4. `/etc/geneos/flashduty.yaml`;
5. compiled defaults.

An explicitly supplied but unreadable `--config` path is an error. Missing implicit configuration files are not errors.
The selected YAML file is overlaid on the compiled defaults by field, so omitted fields and severity mappings retain their default values.

Example:

```yaml
flashduty:
  endpoint: https://api.flashcat.cloud/event/push/alert/standard
  integration_key: ""
  timeout: 10s
  retries: 3

  alert_key:
    prefix: "geneos:v1:"

  severity_map:
    CRITICAL: Critical
    WARNING: Warning
    OK: Ok
    UNDEFINED: Info

  title: "${_RULE} Triggered"
  description: "entity=${_MANAGED_ENTITY}, value=${_VALUE}, path=${_VARIABLEPATH}"
```

`${NAME}` references in `title`, `description`, and user-configured label values are expanded from the process environment. Endpoint, integration key, timeout, retry, key prefix, and severity-map values are not template-expanded. An unset template variable expands to an empty string.

The integration key precedence is:

1. `FLASHDUTY_INTEGRATION_KEY` environment variable when non-empty;
2. `flashduty.integration_key` from YAML.

The program fails before making a request when both are empty. The integration key is passed as the `integration_key` query parameter and is always redacted from logs and error messages.

Trigger and recovery events must resolve to the same integration key as well as the same `alert_key`. Changing `FLASHDUTY_INTEGRATION_KEY` or the YAML integration key between those events routes them into different FlashDuty integrations and prevents lifecycle correlation; this operational constraint is documented in the README.

## Geneos Context

The adapter recognizes these Geneos environment variables:

- `_ALERT_TYPE`
- `_ALERT_TIME`
- `_COLUMN`
- `_DATAVIEW`
- `_GATEWAY`
- `_HOSTNAME`
- `_MANAGED_ENTITY`
- `_NETPROBE_HOST`
- `_PREVIOUS_SEV`
- `_PROBE`
- `_ROWNAME`
- `_RULE`
- `_SAMPLER`
- `_SAMPLER_GROUP`
- `_SEVERITY`
- `_VALUE`
- `_VARIABLE`
- `_VARIABLEPATH`

Only an explicit allow-list of these fields is mapped into the outbound payload. Unknown environment variables are not sent.

## Alert Identity

The same Geneos data item must always generate the same FlashDuty `alert_key`, regardless of current value, severity, alert type, or event time. Those changing properties must never participate in identity calculation.

### Primary identity

When `_VARIABLEPATH` or `--variable-path` is non-empty, the exact UTF-8 value as received is hashed without case conversion or structural normalization:

```text
alert_key = "geneos:v1:" + lowercase_hex(SHA256(variable_path))
```

The hash uses the complete path even when the path is too long to include fully in a label.

### Stable fallback identity

When no variable path exists, the adapter constructs a canonical string from the following non-empty fields in this fixed order:

```text
_GATEWAY
_PROBE
_MANAGED_ENTITY
_SAMPLER
_DATAVIEW
_ROWNAME
_COLUMN
_VARIABLE
```

Each component is encoded as `NAME=length:value`, then components are joined with `|`. Length-prefixing prevents ambiguous concatenations. The fallback is considered sufficient when `_GATEWAY` is present and at least one monitored-location field among `_MANAGED_ENTITY`, `_SAMPLER`, `_DATAVIEW`, `_ROWNAME`, `_COLUMN`, or `_VARIABLE` is present.

```text
alert_key = "geneos:v1:fallback:" + lowercase_hex(SHA256(canonical_components))
```

### Random final fallback

When neither primary nor sufficient stable fallback identity can be constructed, the adapter generates a UUID v4 using `crypto/rand`:

```text
alert_key = "geneos:v1:random:" + uuid
```

The event is still sent, but the process emits a warning that a later recovery event cannot be guaranteed to correlate with the triggered alert. Random identity is never persisted.

The payload includes an `alert_key_source` label with one of `variable_path`, `fallback`, or `random`.

## Event Status Mapping

Status selection uses this precedence:

1. the explicit `resolve` command maps to `Ok`;
2. `_ALERT_TYPE`, compared case-insensitively, equal to `clear` maps to `Ok`;
3. `_SEVERITY`, through `severity_map`, equal to `OK` maps to `Ok`;
4. the explicit `trigger` command uses mapped Critical, Warning, or Info, defaulting to Warning;
5. automatic mode uses the configured severity mapping;
6. an unmapped or empty automatic severity maps to `Info`.

The compiled default mapping is:

| Geneos severity | FlashDuty status |
| --- | --- |
| `CRITICAL`, `3` | `Critical` |
| `WARNING`, `2` | `Warning` |
| `OK`, `1` | `Ok` |
| `UNDEFINED`, `0`, empty, or unknown | `Info` |

Geneos `_ALERT_TYPE=suspend` and assignment-related events do not map to acknowledgement. They retain the severity-derived active status. Incident acknowledgement and responder workflow remain FlashDuty responsibilities.

## FlashDuty Payload

The endpoint receives `POST` with `Content-Type: application/json` and this shape:

```json
{
  "event_status": "Critical",
  "alert_key": "geneos:v1:...",
  "title_rule": "CPU threshold Triggered",
  "description": "entity=Payment-Service, value=95%, path=...",
  "labels": {
    "source": "geneos",
    "gateway": "PROD",
    "probe": "host01",
    "managed_entity": "Payment-Service",
    "sampler": "CPU",
    "sampler_group": "System",
    "dataview": "CPU",
    "row": "cpu0",
    "column": "utilisation",
    "rule": "CPU threshold",
    "geneos_severity": "CRITICAL",
    "geneos_alert_type": "alert",
    "geneos_variable_path": "/geneos/gateway[...]",
    "alert_key_source": "variable_path"
  }
}
```

Empty labels are omitted. The adapter enforces FlashDuty protocol limits before sending:

- title: 512 characters;
- description: 2048 characters;
- label key: 128 characters;
- label value: 2048 characters;
- label count: 50.

Truncation is Unicode-safe and produces a warning. A long `geneos_variable_path` label may be truncated, but its full untruncated value is used to compute `alert_key`.

## HTTP and Retry Behavior

- The default per-attempt timeout is 10 seconds.
- `retries: 3` means one initial attempt plus up to three retries.
- Transport errors, HTTP 429, and HTTP 5xx are retryable.
- HTTP 400, 401, 403, 404, and other 4xx responses are not retryable.
- Retry delay uses exponential backoff with jitter, starting at one second and capped at ten seconds.
- A valid `Retry-After` response header overrides the calculated delay, capped at 60 seconds.
- Any HTTP 2xx response is treated as transport success. If the response contains a FlashDuty error object, it is treated as a failed request.
- A successful response logs the FlashDuty `request_id` when present.
- Logs include event status, alert key, identity source, attempt number, HTTP status, and request ID, but never the integration key.
- Process exit code is zero after successful delivery and non-zero after validation or final delivery failure.

This exit behavior allows Geneos repeat-until-success or external supervision to detect delivery failure.

## Error Handling

Configuration, template, payload, and request errors are returned with concise context. The adapter will not panic for malformed configuration, missing environment variables, malformed HTTP responses, or network failures.

If random identity is required, the adapter continues with a warning. Missing integration credentials, invalid endpoint configuration, invalid timeout/retry values, and failure to obtain secure random bytes are fatal.

## Testing

Unit tests will cover:

- identical primary identity for trigger and recovery from the same `_VARIABLEPATH`;
- different keys for different Geneos cells;
- full-path hashing even when the label is truncated;
- deterministic stable fallback encoding and hashing;
- UUID final fallback and its warning;
- status precedence for Effect clear, Action OK, explicit trigger, and explicit resolve;
- default and custom severity mapping;
- YAML loading and environment integration-key override;
- template expansion;
- payload omission and Unicode-safe length enforcement;
- integration-key redaction;
- retry behavior for network errors, 429, and 5xx;
- absence of retries for 4xx;
- successful response and request ID handling.

Integration-style tests will use `httptest.Server` to validate HTTP method, query parameter, headers, JSON payload, retry count, and trigger/recovery key equality without contacting FlashDuty.

## Deliverables

The project will contain:

- Go module and source code;
- unit and HTTP integration tests;
- `flashduty.example.yaml`;
- Geneos Effect and Action XML examples;
- README with build, installation, configuration, and troubleshooting instructions;
- build commands for Linux AMD64 and ARM64 static executables where supported by the selected dependencies.

## Acceptance Criteria

The implementation is accepted when:

1. `go test ./...` passes;
2. `go vet ./...` passes;
3. the Linux AMD64 binary builds successfully;
4. a Critical trigger and an Ok recovery using the same `_VARIABLEPATH` produce identical `alert_key` values in tests;
5. both Effect clear and Action severity-OK flows produce `event_status=Ok`;
6. the raw variable path is present in labels when supplied;
7. missing variable path follows deterministic fallback, then random fallback without blocking delivery;
8. no test or normal log output exposes the configured integration key;
9. example configurations document both supported Geneos invocation models.
