package diagnosis

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kubewhy/kubewhy/internal/model"
)

// Engine correlates pod state, events, logs, and resource context into an
// explanation. It has no Kubernetes client dependency by design.
type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Diagnose(in model.DiagnoseRequest) model.Report {
	pod := in.Pod
	report := model.Report{
		GeneratedAt: time.Now().UTC(),
		Pod: model.PodIdentity{
			Name: pod.Metadata.Name, Namespace: pod.Metadata.Namespace,
			UID: pod.Metadata.UID, Phase: pod.Status.Phase, Node: pod.Spec.NodeName,
		},
		Reasons: []model.Reason{}, Containers: []model.ContainerFinding{},
		RelevantEvents: []model.EventFinding{}, ResourceFindings: []model.ResourceFinding{},
	}

	checkPodState(&report, pod)
	checkContainers(&report, pod)
	checkEvents(&report, in.Events)
	checkLogs(&report, in.Logs)
	checkResources(&report, pod, in.Resources)

	sort.SliceStable(report.Reasons, func(i, j int) bool {
		return severityRank(report.Reasons[i].Severity) > severityRank(report.Reasons[j].Severity)
	})
	status := "healthy"
	if len(report.Reasons) > 0 {
		status = "degraded"
		for _, reason := range report.Reasons {
			if reason.Severity == "critical" || reason.Severity == "error" {
				status = "broken"
				break
			}
		}
	}
	if pod.Status.Phase == "Unknown" || pod.Status.Phase == "" {
		if len(report.Reasons) == 0 {
			status = "unknown"
		}
	}
	report.Status = status
	name := pod.Metadata.Name
	if name == "" {
		name = "<unnamed>"
	}
	switch len(report.Reasons) {
	case 0:
		report.Summary = fmt.Sprintf("Pod %s is healthy based on the supplied state.", name)
	case 1:
		report.Summary = fmt.Sprintf("Pod %s is %s: 1 reason found.", name, status)
	default:
		report.Summary = fmt.Sprintf("Pod %s is %s: %d reasons found.", name, status, len(report.Reasons))
	}
	return report
}

func checkPodState(report *model.Report, pod model.Pod) {
	if pod.Status.Phase == "Failed" {
		explanation := pod.Status.Message
		if explanation == "" {
			explanation = "Kubernetes marked the pod as failed."
		}
		evidence := []string{"phase=Failed"}
		if pod.Status.Reason != "" {
			evidence = append(evidence, "reason="+pod.Status.Reason)
		}
		addReason(report, model.Reason{Code: "pod_failed", Severity: "critical", Title: "Pod has failed", Explanation: explanation,
			Evidence: evidence, Remediation: []string{"Inspect the container and event findings below", "Roll back or redeploy after fixing the reported failure"}})
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Status == "False" && (condition.Type == "Ready" || condition.Type == "ContainersReady" || condition.Type == "PodScheduled") {
			evidence := []string{condition.Type + "=False"}
			if condition.Reason != "" {
				evidence = append(evidence, "reason="+condition.Reason)
			}
			if condition.Message != "" {
				evidence = append(evidence, condition.Message)
			}
			severity := "error"
			if condition.Type == "Ready" {
				severity = "warning"
			}
			addReason(report, model.Reason{Code: "condition_" + strings.ToLower(condition.Type), Severity: severity,
				Title: condition.Type + " condition is false", Explanation: "The pod condition reports that this part of the pod lifecycle is not healthy.", Evidence: evidence,
				Remediation: []string{"Use the condition message and related events to identify the failing dependency"}})
		}
	}
}

func checkContainers(report *model.Report, pod model.Pod) {
	check := func(statuses []model.ContainerStatus, kind string) {
		for _, status := range statuses {
			finding := model.ContainerFinding{Name: status.Name, Kind: kind, Ready: status.Ready, RestartCount: status.RestartCount, Details: []string{}}
			state, details, reason := describeState(status)
			finding.State, finding.Details = state, details
			report.Containers = append(report.Containers, finding)
			if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "ErrImagePull" || reason == "CreateContainerConfigError" || reason == "CreateContainerError" || reason == "InvalidImageName" {
				severity := "critical"
				if reason == "CreateContainerConfigError" {
					severity = "error"
				}
				addReason(report, model.Reason{Code: codeForReason(reason), Severity: severity, Title: titleForReason(reason),
					Explanation: explanationForReason(reason), Evidence: append([]string{"container=" + status.Name, "waiting reason=" + reason}, details...),
					Remediation: remediationForReason(reason)})
			}
			if status.LastState.Terminated != nil && status.LastState.Terminated.Reason == "OOMKilled" {
				addReason(report, model.Reason{Code: "oom_killed", Severity: "critical", Title: "Container was killed for exceeding memory", Explanation: "The previous container process exceeded its memory limit and the kernel terminated it.",
					Evidence: []string{"container=" + status.Name, "last termination reason=OOMKilled"}, Remediation: []string{"Increase the memory limit only after checking the workload's actual usage", "Profile the process for leaks or unexpectedly large input"}})
			}
			if status.LastState.Terminated != nil && status.LastState.Terminated.ExitCode != 0 && reason == "" {
				terminated := status.LastState.Terminated
				addReason(report, model.Reason{Code: "container_exit", Severity: "error", Title: "Container exited unsuccessfully", Explanation: "The container's last process exited with a non-zero status.",
					Evidence: []string{"container=" + status.Name, fmt.Sprintf("exitCode=%d", terminated.ExitCode), nonEmpty(terminated.Reason, terminated.Message)}, Remediation: []string{"Inspect current and previous logs", "Verify command, arguments, configuration, and downstream dependencies"}})
			}
			if !status.Ready && state == "running" && reason == "" {
				addReason(report, model.Reason{Code: "not_ready", Severity: "warning", Title: "Running container is not ready", Explanation: "The process is running, but its readiness signal has not succeeded.", Evidence: []string{"container=" + status.Name, "ready=false"}, Remediation: []string{"Check readiness probe path, port, credentials, and dependent services"}})
			}
		}
	}
	check(pod.Status.InitContainerStatuses, "init")
	check(pod.Status.ContainerStatuses, "app")
}

func describeState(status model.ContainerStatus) (string, []string, string) {
	if status.State.Waiting != nil {
		return "waiting", []string{nonEmpty(status.State.Waiting.Message, "no waiting message")}, status.State.Waiting.Reason
	}
	if status.State.Terminated != nil {
		t := status.State.Terminated
		return "terminated", []string{fmt.Sprintf("exitCode=%d", t.ExitCode), nonEmpty(t.Reason, t.Message)}, t.Reason
	}
	if status.State.Running != nil {
		return "running", []string{"ready=" + fmt.Sprint(status.Ready)}, ""
	}
	return "unknown", []string{"no current state reported"}, ""
}

func checkEvents(report *model.Report, events []model.Event) {
	for _, event := range events {
		if !isRelevantEvent(event) {
			continue
		}
		severity := "warning"
		if strings.EqualFold(event.Type, "Warning") && isHardEvent(event.Reason) {
			severity = "error"
		}
		report.RelevantEvents = append(report.RelevantEvents, model.EventFinding{Type: event.Type, Reason: event.Reason, Message: event.Message, Count: event.Count, Severity: severity})
		evidence := []string{nonEmpty(event.Reason, "event") + ": " + event.Message}
		if event.Count > 1 {
			evidence = append(evidence, fmt.Sprintf("count=%d", event.Count))
		}
		code := "event_" + strings.ToLower(event.Reason)
		if event.Reason == "FailedScheduling" || event.Reason == "FailedMount" || event.Reason == "FailedAttachVolume" || event.Reason == "FailedCreatePodSandBox" {
			addReason(report, model.Reason{Code: code, Severity: "error", Title: "Kubernetes reported a blocking event", Explanation: "A Kubernetes event describes a failure that can prevent the pod from being scheduled, started, or mounted.", Evidence: evidence, Remediation: []string{"Resolve the condition named in the event", "Recheck the pod after the controller retries"}})
		} else if event.Reason == "BackOff" || event.Reason == "Unhealthy" || event.Reason == "Failed" {
			addReason(report, model.Reason{Code: code, Severity: "warning", Title: "Kubernetes reported a warning event", Explanation: "The event is a direct signal from the scheduler, kubelet, or controller about pod health.", Evidence: evidence, Remediation: []string{"Use the event message as the next investigation step", "Inspect the matching container details and logs"}})
		}
	}
}

func checkLogs(report *model.Report, logs []model.ContainerLog) {
	patterns := []struct {
		code, title, explanation string
		severity                 string
		needles                  []string
		remediation              []string
	}{
		{"panic", "Application panic found in logs", "The application terminated because it hit a panic.", "critical", []string{"panic:", "fatal error:"}, []string{"Fix the panic at the reported stack frame", "Use previous logs to capture the full stack trace"}},
		{"config_error", "Application configuration error found", "The process reports that required configuration is missing or invalid.", "error", []string{"configuration error", "missing required", "invalid configuration", "failed to load config"}, []string{"Verify ConfigMaps, Secrets, environment variables, and mounted files"}},
		{"dependency_unavailable", "Application dependency is unavailable", "The logs contain a connection or dependency failure that can prevent startup or readiness.", "error", []string{"connection refused", "no such host", "i/o timeout", "database unavailable", "service unavailable"}, []string{"Verify the dependency service, DNS, network policy, and credentials"}},
		{"permission_denied", "Application hit a permission error", "The process was denied access to a file, socket, or API operation.", "error", []string{"permission denied", "forbidden", "access denied"}, []string{"Check the container user, filesystem permissions, ServiceAccount, and RBAC rules"}},
	}
	for _, log := range logs {
		lower := strings.ToLower(log.Text)
		for _, pattern := range patterns {
			matched := ""
			for _, needle := range pattern.needles {
				if strings.Contains(lower, needle) {
					matched = needle
					break
				}
			}
			if matched == "" {
				continue
			}
			which := "container=" + log.Container
			if log.Previous {
				which += " previous=true"
			}
			addReason(report, model.Reason{Code: "log_" + pattern.code, Severity: pattern.severity, Title: pattern.title, Explanation: pattern.explanation, Evidence: []string{which, "matched=" + matched}, Remediation: pattern.remediation})
		}
	}
}

func checkResources(report *model.Report, pod model.Pod, context model.ResourceContext) {
	requestCPU, requestMemory := int64(0), int64(0)
	initCPU, initMemory := int64(0), int64(0)
	missingRequests := []string{}
	for _, container := range pod.Spec.Containers {
		cpu, cpuOK := parseQuantity(container.Resources.Requests["cpu"], false)
		memory, memoryOK := parseQuantity(container.Resources.Requests["memory"], true)
		requestCPU += cpu
		requestMemory += memory
		if !cpuOK || !memoryOK {
			missingRequests = append(missingRequests, container.Name)
		}
	}
	// Kubernetes schedules a pod using the sum of app requests versus the
	// maximum init-container request, per resource.
	for _, container := range pod.Spec.InitContainers {
		cpu, cpuOK := parseQuantity(container.Resources.Requests["cpu"], false)
		memory, memoryOK := parseQuantity(container.Resources.Requests["memory"], true)
		if cpu > initCPU {
			initCPU = cpu
		}
		if memory > initMemory {
			initMemory = memory
		}
		if !cpuOK || !memoryOK {
			missingRequests = append(missingRequests, container.Name)
		}
	}
	if initCPU > requestCPU {
		requestCPU = initCPU
	}
	if initMemory > requestMemory {
		requestMemory = initMemory
	}
	if len(missingRequests) > 0 {
		addResourceFinding(report, model.ResourceFinding{Code: "missing_requests", Severity: "warning", Title: "Some containers have incomplete resource requests", Explanation: "Without CPU and memory requests, scheduling and quota behavior can be surprising and the pod is harder to size safely.", Evidence: []string{"containers=" + strings.Join(missingRequests, ", ")}})
	}
	for _, node := range context.Nodes {
		if !node.Schedulable {
			continue
		}
		if node.AvailableCPUMillicores > 0 && requestCPU > node.AvailableCPUMillicores {
			addResourceFinding(report, model.ResourceFinding{Code: "insufficient_cpu", Severity: "error", Title: "Pod requests more CPU than available", Explanation: "The supplied node context does not have enough available CPU for these requests.", Evidence: []string{fmt.Sprintf("requested=%dm available=%dm node=%s", requestCPU, node.AvailableCPUMillicores, node.Name)}})
		}
		if node.AvailableMemoryBytes > 0 && requestMemory > node.AvailableMemoryBytes {
			addResourceFinding(report, model.ResourceFinding{Code: "insufficient_memory", Severity: "error", Title: "Pod requests more memory than available", Explanation: "The supplied node context does not have enough available memory for these requests.", Evidence: []string{fmt.Sprintf("requested=%d available=%d node=%s", requestMemory, node.AvailableMemoryBytes, node.Name)}})
		}
	}
	for _, quota := range context.Quotas {
		if quota.HardCPUMillicores > 0 && quota.UsedCPUMillicores+requestCPU > quota.HardCPUMillicores {
			addResourceFinding(report, model.ResourceFinding{Code: "cpu_quota", Severity: "error", Title: "Pod would exceed CPU quota", Explanation: "The namespace quota has less CPU headroom than this pod requests.", Evidence: []string{fmt.Sprintf("quota=%s used=%dm requested=%dm hard=%dm", quota.Name, quota.UsedCPUMillicores, requestCPU, quota.HardCPUMillicores)}})
		}
		if quota.HardMemoryBytes > 0 && quota.UsedMemoryBytes+requestMemory > quota.HardMemoryBytes {
			addResourceFinding(report, model.ResourceFinding{Code: "memory_quota", Severity: "error", Title: "Pod would exceed memory quota", Explanation: "The namespace quota has less memory headroom than this pod requests.", Evidence: []string{fmt.Sprintf("quota=%s used=%d requested=%d hard=%d", quota.Name, quota.UsedMemoryBytes, requestMemory, quota.HardMemoryBytes)}})
		}
	}
}

func addReason(report *model.Report, reason model.Reason) {
	for _, existing := range report.Reasons {
		if existing.Code == reason.Code {
			return
		}
	}
	report.Reasons = append(report.Reasons, reason)
}
func addResourceFinding(report *model.Report, finding model.ResourceFinding) {
	report.ResourceFindings = append(report.ResourceFindings, finding)
	severity := finding.Severity
	addReason(report, model.Reason{Code: "resource_" + finding.Code, Severity: severity, Title: finding.Title, Explanation: finding.Explanation, Evidence: finding.Evidence, Remediation: []string{"Review resource requests, limits, node capacity, and namespace quotas"}})
}
func severityRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "error":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}
func nonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}
func isRelevantEvent(event model.Event) bool {
	return strings.EqualFold(event.Type, "Warning") || isHardEvent(event.Reason) || event.Reason == "BackOff" || event.Reason == "Unhealthy"
}
func isHardEvent(reason string) bool {
	switch reason {
	case "FailedScheduling", "FailedMount", "FailedAttachVolume", "FailedCreatePodSandBox", "Failed":
		return true
	default:
		return false
	}
}
func codeForReason(reason string) string {
	return map[string]string{"CrashLoopBackOff": "crash_loop", "ImagePullBackOff": "image_pull_backoff", "ErrImagePull": "image_pull", "CreateContainerConfigError": "container_config", "CreateContainerError": "container_create", "InvalidImageName": "invalid_image"}[reason]
}
func titleForReason(reason string) string {
	return map[string]string{"CrashLoopBackOff": "Container is crash-looping", "ImagePullBackOff": "Image pull is backing off", "ErrImagePull": "Container image could not be pulled", "CreateContainerConfigError": "Container configuration could not be created", "CreateContainerError": "Container could not be created", "InvalidImageName": "Container image name is invalid"}[reason]
}
func explanationForReason(reason string) string {
	return map[string]string{"CrashLoopBackOff": "The container terminated repeatedly and Kubernetes is backing off restarts.", "ImagePullBackOff": "Kubernetes could not pull the image and is increasing the delay between retries.", "ErrImagePull": "The kubelet failed to pull the configured image.", "CreateContainerConfigError": "Kubernetes could not construct the container configuration, often because a referenced Secret or ConfigMap key is missing.", "CreateContainerError": "The runtime failed while creating the container.", "InvalidImageName": "The image reference does not match a valid image name."}[reason]
}
func remediationForReason(reason string) []string {
	switch reason {
	case "CrashLoopBackOff":
		return []string{"Inspect previous container logs", "Verify the command, arguments, probes, and required dependencies"}
	case "ImagePullBackOff", "ErrImagePull":
		return []string{"Verify image name and tag", "Check registry credentials and network access from the node"}
	case "CreateContainerConfigError":
		return []string{"Check referenced Secrets and ConfigMaps and their key names"}
	case "InvalidImageName":
		return []string{"Correct the image repository, tag, and registry syntax"}
	default:
		return []string{"Inspect kubelet and runtime details for the container"}
	}
}
