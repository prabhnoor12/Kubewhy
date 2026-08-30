package model

import "time"

// DiagnoseRequest is the transport-neutral input to the diagnostic engine.
// Pod follows the shape of a Kubernetes Pod but only models fields needed by
// the checks. Unknown fields are intentionally ignored by encoding/json.
type DiagnoseRequest struct {
	Pod              Pod             `json:"pod"`
	Events           []Event         `json:"events,omitempty"`
	Logs             []ContainerLog  `json:"logs,omitempty"`
	CollectionErrors []string        `json:"collectionErrors,omitempty"`
	Resources        ResourceContext `json:"resources,omitempty"`
}

type Pod struct {
	APIVersion string     `json:"apiVersion,omitempty"`
	Kind       string     `json:"kind,omitempty"`
	Metadata   ObjectMeta `json:"metadata"`
	Spec       PodSpec    `json:"spec"`
	Status     PodStatus  `json:"status"`
}

type ObjectMeta struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	UID       string `json:"uid,omitempty"`
}

type PodSpec struct {
	NodeName           string      `json:"nodeName,omitempty"`
	RestartPolicy      string      `json:"restartPolicy,omitempty"`
	Containers         []Container `json:"containers"`
	InitContainers     []Container `json:"initContainers,omitempty"`
	ActiveDeadlineSecs *int64      `json:"activeDeadlineSeconds,omitempty"`
}

type Container struct {
	Name      string               `json:"name"`
	Image     string               `json:"image,omitempty"`
	Command   []string             `json:"command,omitempty"`
	Args      []string             `json:"args,omitempty"`
	Resources ResourceRequirements `json:"resources,omitempty"`
}

type ResourceRequirements struct {
	Requests map[string]string `json:"requests,omitempty"`
	Limits   map[string]string `json:"limits,omitempty"`
}

type PodStatus struct {
	Phase                 string            `json:"phase,omitempty"`
	Reason                string            `json:"reason,omitempty"`
	Message               string            `json:"message,omitempty"`
	StartTime             *time.Time        `json:"startTime,omitempty"`
	Conditions            []PodCondition    `json:"conditions,omitempty"`
	ContainerStatuses     []ContainerStatus `json:"containerStatuses,omitempty"`
	InitContainerStatuses []ContainerStatus `json:"initContainerStatuses,omitempty"`
}

type PodCondition struct {
	Type               string    `json:"type"`
	Status             string    `json:"status"`
	Reason             string    `json:"reason,omitempty"`
	Message            string    `json:"message,omitempty"`
	LastTransitionTime time.Time `json:"lastTransitionTime,omitempty"`
}

type ContainerStatus struct {
	Name         string         `json:"name"`
	Ready        bool           `json:"ready"`
	RestartCount int32          `json:"restartCount"`
	State        ContainerState `json:"state"`
	LastState    ContainerState `json:"lastState"`
}

type ContainerState struct {
	Waiting    *WaitingState    `json:"waiting,omitempty"`
	Running    *RunningState    `json:"running,omitempty"`
	Terminated *TerminatedState `json:"terminated,omitempty"`
}

type WaitingState struct {
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

type RunningState struct {
	StartedAt *time.Time `json:"startedAt,omitempty"`
}

type TerminatedState struct {
	ExitCode   int32      `json:"exitCode,omitempty"`
	Signal     int32      `json:"signal,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Message    string     `json:"message,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type Event struct {
	Type      string `json:"type,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	Count     int32  `json:"count,omitempty"`
	FirstTime string `json:"firstTimestamp,omitempty"`
	LastTime  string `json:"lastTimestamp,omitempty"`
	Source    string `json:"source,omitempty"`
}

type ContainerLog struct {
	Container string `json:"container"`
	Previous  bool   `json:"previous,omitempty"`
	Text      string `json:"text"`
}

type ResourceContext struct {
	Nodes  []NodeCapacity `json:"nodes,omitempty"`
	Quotas []QuotaStatus  `json:"quotas,omitempty"`
}

type NodeCapacity struct {
	Name                   string `json:"name"`
	Schedulable            bool   `json:"schedulable"`
	AvailableCPUMillicores int64  `json:"availableCpuMillicores,omitempty"`
	AvailableMemoryBytes   int64  `json:"availableMemoryBytes,omitempty"`
}

type QuotaStatus struct {
	Name              string `json:"name"`
	HardCPUMillicores int64  `json:"hardCpuMillicores,omitempty"`
	UsedCPUMillicores int64  `json:"usedCpuMillicores,omitempty"`
	HardMemoryBytes   int64  `json:"hardMemoryBytes,omitempty"`
	UsedMemoryBytes   int64  `json:"usedMemoryBytes,omitempty"`
}

type Report struct {
	GeneratedAt      time.Time          `json:"generatedAt"`
	Pod              PodIdentity        `json:"pod"`
	Status           string             `json:"status"`
	Confidence       string             `json:"confidence"`
	MissingContext   []string           `json:"missingContext,omitempty"`
	Summary          string             `json:"summary"`
	RootCause        *Reason            `json:"rootCause,omitempty"`
	Reasons          []Reason           `json:"reasons"`
	Containers       []ContainerFinding `json:"containers"`
	RelevantEvents   []EventFinding     `json:"relevantEvents"`
	ResourceFindings []ResourceFinding  `json:"resourceFindings"`
}

type PodIdentity struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	UID       string `json:"uid,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Node      string `json:"node,omitempty"`
}

type Reason struct {
	Code        string   `json:"code"`
	Severity    string   `json:"severity"`
	Confidence  string   `json:"confidence"`
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	Evidence    []string `json:"evidence"`
	Remediation []string `json:"remediation"`
}

type ContainerFinding struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Ready        bool     `json:"ready"`
	RestartCount int32    `json:"restartCount"`
	State        string   `json:"state"`
	Details      []string `json:"details"`
}

type EventFinding struct {
	Type     string `json:"type"`
	Reason   string `json:"reason"`
	Message  string `json:"message"`
	Count    int32  `json:"count,omitempty"`
	Severity string `json:"severity"`
}

type ResourceFinding struct {
	Code        string   `json:"code"`
	Severity    string   `json:"severity"`
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	Evidence    []string `json:"evidence"`
}
