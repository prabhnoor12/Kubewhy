# kubewhy

kubewhy explains why a Kubernetes pod is unhealthy by correlating the information operators normally collect separately:

- pod phase, conditions, and container state
- warning events and scheduler messages
- current and previous container logs
- resource requests, limits, node capacity, and quota context

The diagnostic engine is a pure Go package. It does not need a cluster connection, so it can be used by a CLI, an HTTP service, a controller, or CI. The included API accepts a Kubernetes pod document and optional context gathered from `kubectl` or another integration.

## Quick start

```powershell
go run ./cmd/kubewhy diagnose --file examples/crashloop-request.json
go run ./cmd/kubewhy diagnose --pod api --namespace payments
go run ./cmd/kubewhy diagnose --pod api --namespace payments --watch --interval 5s
go run ./cmd/kubewhy diagnose --file examples/crashloop-request.json --exit-code
go run ./cmd/kubewhy serve --listen :8080
```

The CLI writes human-readable output by default. Add `--json` for machine-readable output. Pod mode is read-only: it uses the current kubeconfig context by default, collects the pod, namespace events, and bounded logs, and supports `--context`, `--kubeconfig`, `--tail`, and `--previous`. Add `--watch` to repeat collection until Ctrl+C; `--interval` defaults to five seconds. Watch mode with `--json` emits newline-delimited JSON reports.

Use `--exit-code` in CI or shell automation. The stable mapping is `0=healthy`, `1=degraded`, `2=broken`, and `3=unknown`.

## API

### `POST /api/v1/diagnose`

The request body is:

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

`POST /api/v1/diagnose/pod` is an alias. `GET /healthz` returns service health.

The response contains an overall status and summary, ranked reasons, per-container details, relevant events, and resource findings:

```json
{
  "status": "broken",
  "confidence": "medium",
  "summary": "Pod payments/api is broken: 2 reasons found.",
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

`confidence` describes how strongly the supplied evidence supports the report. A report with omitted diagnostic inputs is never marked healthy. Instead, it uses `status: "unknown"`, `confidence: "low"`, and lists the missing inputs in `missingContext`; explicitly supplied empty arrays such as `"events": []` mean that collection was performed and found no items.

`rootCause` is the highest-ranked likely cause. The `reasons` array keeps the complete ranked explanation, including symptoms such as readiness failures and restart backoff.

When cluster log collection fails for one or more containers, the collector preserves those failures in `collectionErrors` so the diagnosis remains low-confidence instead of silently treating missing logs as a clean result.

## kubectl plugin

Build the plugin binary and place it on `PATH` next to `kubectl`:

```powershell
go build -o kubectl-kubewhy.exe ./cmd/kubectl-kubewhy
kubectl kubewhy diagnose pod/api -n payments
kubectl kubewhy diagnose pod/api -n payments --watch --interval 5s
```

The plugin accepts Kubernetes-style `pod/name` and `-n` arguments, then uses the same read-only collector and diagnosis engine as the main binary.

## Supplying context from kubectl

kubewhy deliberately separates collection from diagnosis. A thin integration can collect the data using commands such as:

```powershell
kubectl get pod api -n payments -o json | Set-Content pod.json
kubectl get events -n payments --field-selector involvedObject.name=api -o json | Set-Content events.json
kubectl logs api -n payments --all-containers --prefix --tail=200 | Set-Content logs.txt
```

Then wrap those documents in the API request shape above. This avoids granting the explanation service cluster credentials by default, while leaving room for a future collector module.

## Project layout

```text
cmd/kubewhy/          CLI entry point
internal/api/         HTTP transport and request validation
internal/diagnosis/   Correlation engine and checks
internal/model/       Kubernetes input and report contracts
examples/             Reproducible sample input
```

## Development

```powershell
gofmt -w cmd internal
go test ./...
```
