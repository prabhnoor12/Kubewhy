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
go run ./cmd/kubewhy serve --listen :8080
```

The CLI writes human-readable output by default. Add `--json` for machine-readable output.

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
  "summary": "Pod payments/api is broken: 2 high-confidence reasons found.",
  "reasons": [
    {
      "code": "crash_loop",
      "severity": "critical",
      "title": "Container is crash-looping",
      "explanation": "The api container terminated repeatedly and Kubernetes is backing off restarts.",
      "evidence": ["waiting reason=CrashLoopBackOff", "restartCount=7"],
      "remediation": ["Inspect previous logs", "Fix the startup failure before redeploying"]
    }
  ]
}
```

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
