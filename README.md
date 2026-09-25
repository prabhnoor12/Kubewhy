# kubewhy

[![CI](https://github.com/kubewhy/kubewhy/actions/workflows/ci.yml/badge.svg)](https://github.com/kubewhy/kubewhy/actions/workflows/ci.yml)

kubewhy explains why a Kubernetes pod is unhealthy. Point it at a broken pod and it correlates pod state, container statuses, events, logs, and resource context into a ranked diagnosis with root cause, evidence, and remediation steps.

It ships as a **CLI**, a **kubectl plugin**, an **HTTP API server**, and a **VS Code extension**.

## Features

- **Automated diagnosis** — collects pod spec, status, events, and logs, then runs a 5-phase correlation engine that detects 25+ failure modes
- **Smart root cause ranking** — root causes sort above symptoms (OOMKilled ranks above CrashLoopBackOff, not the other way around)
- **LLM explanations** — send the structured report to any OpenAI-compatible API for plain-language debugging advice with copy-paste kubectl commands
- **Watch mode** — continuous monitoring with state-change detection; only alerts when the status transitions
- **History tracking** — every diagnosis is saved to `~/.kubewhy/history.jsonl` for querying and auditing
- **VS Code extension** — diagnose, explain, and watch pods from your editor with a rich webview report and sidebar history
- **API server** — HTTP endpoint with per-IP rate limiting, request IDs, and Prometheus metrics
- **CI/CD friendly** — `--exit-code` and `--json` flags for pipeline integration
- **Read-only** — never mutates cluster state; collects pod, events, and logs only

## Quick start

```bash
go install github.com/kubewhy/kubewhy/cmd/kubewhy@latest
kubewhy diagnose --file examples/crashloop-request.json
```

Requires Go 1.23+. For the kubectl plugin:

```bash
go install github.com/kubewhy/kubewhy/cmd/kubectl-kubewhy@latest
kubectl kubewhy diagnose pod/api -n payments
```

Tagged releases publish ready-to-download archives for Linux, macOS, and Windows (amd64/arm64) on the [Releases page](https://github.com/kubewhy/kubewhy/releases).

## 60-second demo

Try the full diagnosis without a Kubernetes cluster:

```bash
git clone https://github.com/kubewhy/kubewhy.git
cd kubewhy
go run ./cmd/kubewhy diagnose --file examples/crashloop-request.json
```

The sample describes a pod in `CrashLoopBackOff`. kubewhy combines pod status, restart count, warning event, and previous log into a ranked explanation:

```text
BROKEN (confidence: high)
Pod payments/api is broken: 3 reasons found.
namespace: payments
node: worker-1

Root cause: Container is crash-looping (confidence: high)

[CRITICAL] Container is crash-looping (crash_loop)
  The api container terminated repeatedly and Kubernetes is backing off restarts.
  evidence: waiting reason=CrashLoopBackOff; restartCount=7
  next: Inspect previous logs; Fix the startup failure before redeploying
```

## CLI usage

### Diagnose a pod

```bash
# From a JSON file (no cluster needed)
kubewhy diagnose --file request.json

# From a live cluster (read-only collection)
kubewhy diagnose --pod api --namespace payments

# Include previous container logs (for crash-looped containers)
kubewhy diagnose --pod api -n payments --previous

# Machine-readable JSON output
kubewhy diagnose --pod api -n payments --json

# CI/CD integration with exit codes
kubewhy diagnose --pod api -n payments --exit-code
#   exit 0 = healthy, 1 = degraded, 2 = broken, 3 = unknown
```

### LLM explanation

Send the full report to an LLM for a plain-language explanation with actionable kubectl commands:

```bash
export KUBEWHY_LLM_API_KEY="sk-..."
kubewhy diagnose --pod api -n payments --explain
```

Works with any OpenAI-compatible API:

```bash
kubewhy diagnose --pod api -n payments --explain \
  --llm-url https://api.openai.com/v1 \
  --llm-model gpt-4o-mini \
  --llm-key "$API_KEY"
```

Environment variables: `KUBEWHY_LLM_BASE_URL`, `KUBEWHY_LLM_API_KEY`, `KUBEWHY_LLM_MODEL`.

### Watch mode

Continuously monitor a pod with state-change detection:

```bash
kubewhy diagnose --pod api -n payments --watch --interval 10s
```

- Re-diagnoses on the configured interval (default 5s)
- Only prints when the status **transitions** (e.g., `HEALTHY -> BROKEN`)
- Shows how long the pod was in the previous state
- With `--explain`, only calls the LLM on state changes (saves tokens)
- Exponential backoff on collection errors (up to 5 minutes)
- `--json` emits newline-delimited JSON reports
- Colored output: green (healthy), yellow (degraded), red (broken), gray (unknown)

### History

Every diagnosis is automatically saved to `~/.kubewhy/history.jsonl`:

```bash
kubewhy history                              # last 24h
kubewhy history --pod api                    # filter by pod
kubewhy history --namespace payments         # filter by namespace
kubewhy history --status broken              # filter by status
kubewhy history --since 7d --last 10         # last 10 entries in 7 days
kubewhy history --json                       # JSON lines output
```

### kubectl plugin

```bash
kubectl kubewhy pod/api -n payments
kubectl kubewhy diagnose pod/api -n payments --watch --interval 5s
kubectl kubewhy diagnose pod/api -n payments --explain
```

Accepts `pod/name` and `-n` arguments, then delegates to the same engine.

### All CLI flags

**`diagnose` subcommand:**

| Flag | Default | Description |
|---|---|---|
| `--file` | stdin | JSON request file (offline mode) |
| `--pod` | | Pod name for live cluster collection |
| `--namespace` | `default` | Kubernetes namespace |
| `--kubeconfig` | | Path to kubeconfig file |
| `--context` | current context | Kubeconfig context to use |
| `--tail` | 200 | Log lines per container |
| `--previous` | false | Collect previous container logs |
| `--watch` | false | Continuous monitoring until Ctrl+C |
| `--interval` | 5s | Delay between watch collections |
| `--timeout` | 30s | Per-collection timeout |
| `--json` | false | Machine-readable JSON output |
| `--exit-code` | false | Exit non-zero based on status |
| `--explain` | false | LLM explanation of diagnosis |
| `--llm-url` | `https://api.openai.com/v1` | LLM API base URL |
| `--llm-key` | `$KUBEWHY_LLM_API_KEY` | LLM API key |
| `--llm-model` | `gpt-4o-mini` | LLM model name |

**`serve` subcommand:**

| Flag | Default | Description |
|---|---|---|
| `--listen` | `:8080` | HTTP listen address |
| `--shutdown-timeout` | 10s | Graceful shutdown timeout |

**`history` subcommand:**

| Flag | Default | Description |
|---|---|---|
| `--pod` | | Filter by pod name |
| `--namespace` | | Filter by namespace |
| `--since` | 24h | Show entries newer than duration |
| `--status` | | Filter by status |
| `--last` | 0 (all) | Show only last N entries |
| `--json` | false | Output as JSON lines |

## VS Code extension

The `kubewhy-vscode` extension brings kubewhy into your editor.

### Commands

| Command | Description |
|---|---|
| **Kubewhy: Diagnose Pod** | Prompts for pod/namespace, runs diagnosis, shows a rich HTML report |
| **Kubewhy: Explain Pod with LLM** | Same with LLM-powered explanation |
| **Kubewhy: Watch Pod** | Polls a pod at a configurable interval with status bar indicator |
| **Kubewhy: Refresh History** | Refreshes the sidebar tree view |

### UI components

- **Diagnosis webview** — rich HTML panel showing status badge, root cause, ranked reasons with evidence and remediation, container table, events, and collection errors. Uses your VS Code theme.
- **History sidebar** — "Kubewhy History" tree view in the Explorer panel showing past diagnoses with color-coded status icons. Click any entry to re-diagnose that pod.
- **Status bar** — when watching a pod, shows `Kubewhy: namespace/pod` with color-coded background. Click to stop watching.

### Configuration

| Setting | Default | Description |
|---|---|---|
| `kubewhy.cliPath` | `kubewhy` | Path to the kubewhy binary |
| `kubewhy.defaultNamespace` | `default` | Default namespace |
| `kubewhy.context` | (current) | Kubeconfig context |
| `kubewhy.historyLimit` | 50 | Max history entries in tree view |

### Building the extension

```bash
cd extensions/vscode
npm install
npm run compile
```

Press F5 in VS Code with the `extensions/vscode` folder open to launch the extension development host.

## API server

Run the HTTP server for programmatic access:

```bash
kubewhy serve --listen :8080
```

### Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/diagnose` | Run diagnosis (accepts JSON `DiagnoseRequest`) |
| `POST` | `/api/v1/diagnose/pod` | Alias for the above |
| `GET` | `/healthz` | Health check |
| `GET` | `/metrics` | Prometheus metrics |

### Request example

```json
{
  "pod": { "apiVersion": "v1", "kind": "Pod", "metadata": {}, "status": {}, "spec": {} },
  "events": [
    { "type": "Warning", "reason": "BackOff", "message": "Back-off restarting failed container", "count": 8 }
  ],
  "logs": [
    { "container": "api", "previous": true, "text": "panic: database unavailable" }
  ],
  "resources": {
    "nodes": [
      { "name": "worker-1", "schedulable": true, "availableCpuMillicores": 250, "availableMemoryBytes": 268435456 }
    ],
    "quotas": [
      { "name": "team-quota", "hardCpuMillicores": 2000, "usedCpuMillicores": 1900 }
    ]
  }
}
```

### Response example

```json
{
  "status": "broken",
  "confidence": "high",
  "summary": "Pod payments/api is broken: 3 reasons found.",
  "rootCause": {
    "code": "crash_loop",
    "severity": "critical",
    "confidence": "high",
    "title": "Container is crash-looping"
  },
  "reasons": [
    {
      "code": "crash_loop",
      "severity": "critical",
      "confidence": "high",
      "title": "Container is crash-looping",
      "explanation": "The api container terminated repeatedly and Kubernetes is backing off restarts.",
      "evidence": ["waiting reason=CrashLoopBackOff", "restartCount=7"],
      "remediation": ["Inspect previous logs", "Fix the startup failure before redeploying"]
    }
  ]
}
```

The server includes per-IP rate limiting (10 req/s, burst 20), request ID tracking (`X-Request-ID`), 5 MiB body limit, and Prometheus metrics (`kubewhi_diagnosis_duration_seconds`, `kubewhi_diagnosis_requests_total`, `kubewhi_collection_errors_total`).

## What the diagnosis engine detects

The engine runs five check phases and produces causally-ranked reasons:

### Phase 1: Pod state
- Pod in `Failed` phase (`pod_failed`)
- Pod conditions reporting `False`: `Ready`, `ContainersReady`, `PodScheduled`

### Phase 2: Container states
- **CrashLoopBackOff** — container keeps crashing (`crash_loop`)
- **Image pull failures** — `ImagePullBackOff`, `ErrImagePull`, `InvalidImageName`
- **Container creation errors** — `CreateContainerError`, `CreateContainerConfigError`
- **OOMKilled** — container exceeded its memory limit (`oom_killed`)
- **Non-zero exit codes** — container crashed with an error (`container_exit`)
- **Not ready** — running but failing readiness checks (`not_ready`)

### Phase 3: Events
- `FailedScheduling` — no node can accommodate the pod
- `FailedMount` / `FailedAttachVolume` — storage problems
- `FailedCreatePodSandBox` — container runtime errors
- `BackOff` / `Unhealthy` — probe failures

### Phase 4: Log pattern matching
- **Panics** — `panic:`, `fatal error:`
- **Config errors** — `configuration error`, `missing required`
- **Dependency failures** — `connection refused`, `no such host`, `i/o timeout`
- **Permission errors** — `permission denied`, `access denied`

### Phase 5: Resource analysis
- **Missing resource requests** — containers without CPU/memory requests
- **No feasible node** — no node has enough CPU/memory to schedule the pod
- **Quota exceeded** — pod would exceed namespace CPU/memory quota

### Ranking

Reasons are sorted by causal weight so root causes appear above symptoms:

| Weight | Examples |
|---|---|
| 100 | Resource failures, scheduling/mount events |
| 95 | OOMKilled |
| 90 | Log dependency/config/permission issues |
| 88 | Log panics |
| 85 | Container config/image errors |
| 75 | Container exit with non-zero code |
| 70 | Pod failed, CrashLoopBackOff |
| 30 | Not ready, probe backoff, condition failures |

Duplicate reasons across containers are deduplicated with merged evidence.

## Architecture

```text
cmd/
  kubewhy/              CLI entry point
  kubectl-kubewhy/      kubectl plugin (normalizes args, delegates to CLI)

internal/
  cli/                  CLI command routing, flag parsing, output formatting
  collector/            Read-only Kubernetes data collector (client-go)
  diagnosis/            Correlation engine — 5-phase check pipeline
  model/                Data contracts (DiagnoseRequest, Report, Reason, etc.)
  api/                  HTTP server, middleware, request validation
  history/              JSONL-based diagnosis history store
  llm/                  OpenAI-compatible chat completions client + prompt builder

extensions/
  vscode/               VS Code extension (TypeScript)

examples/               Reproducible sample inputs
```

### Data flow

```
                    +-----------+
  kubeconfig -----> | collector | -----> pod, events, logs
                    +-----------+
                          |
                          v
                    +-----------+
  request.json --> | diagnosis | -----> Report (status, reasons, root cause)
                    +-----------+
                     |    |    |
                     v    v    v
                  human  JSON  LLM
                  output       explanation
                    |
                    v
                +--------+
                | history | -----> ~/.kubewhy/history.jsonl
                +--------+
```

The diagnosis engine is a pure function with no Kubernetes client dependency. The collector handles all cluster interaction separately, which means the engine can be tested with synthetic inputs and reused through the API server without cluster access.

## Distribution

1. **Go install** (requires Go 1.23+):
   ```bash
   go install github.com/kubewhy/kubewhy/cmd/kubewhy@latest
   go install github.com/kubewhy/kubewhy/cmd/kubectl-kubewhy@latest
   ```

2. **Pre-built binaries** from [Releases](https://github.com/kubewhy/kubewhy/releases/latest) — Linux, macOS, Windows on amd64/arm64.

3. **Build from source**:
   ```bash
   go build -o kubewhy ./cmd/kubewhy
   go build -o kubectl-kubewhy ./cmd/kubectl-kubewhy
   ```

## Development

```bash
# Format
gofmt -w cmd internal

# Test
go test ./...

# Run
go run ./cmd/kubewhy diagnose --file examples/crashloop-request.json
go run ./cmd/kubewhy serve --listen :8080
```

CI runs `gofmt`, `go vet`, `golangci-lint`, and tests with the race detector on every push and PR. Releases are built automatically with GoReleaser when a `v*` tag is pushed.

## License

Apache 2.0
