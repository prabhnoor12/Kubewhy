package collector

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubewhy/kubewhy/internal/model"
)

const defaultTailLines int64 = 200
const defaultMaxLogBytes int64 = 1 << 20

// Options controls the amount of read-only context collected for one pod.
type Options struct {
	TailLines    int64
	MaxLogBytes  int64
	PreviousLogs bool
}

// Collector gathers the Kubernetes objects needed by the diagnosis engine.
// It never mutates cluster state.
type Collector struct {
	client kubernetes.Interface
}

func New(client kubernetes.Interface) *Collector {
	return &Collector{client: client}
}

// NewFromKubeconfig uses the requested kubeconfig/context, falling back to
// in-cluster configuration when no external configuration is available.
func NewFromKubeconfig(kubeconfig, contextName string) (*Collector, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfig != "" {
		loadingRules.ExplicitPath = kubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil && kubeconfig == "" && contextName == "" {
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes configuration: %w", err)
	}
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	return New(client), nil
}

// Collect fetches a pod, its namespace events, and bounded logs for each
// container. A log failure for one container does not discard the pod state or
// logs collected from other containers.
func (c *Collector) Collect(ctx context.Context, namespace, podName string, options Options) (model.DiagnoseRequest, error) {
	if c == nil || c.client == nil {
		return model.DiagnoseRequest{}, fmt.Errorf("Kubernetes client is not configured")
	}
	if namespace == "" {
		namespace = "default"
	}
	if options.TailLines <= 0 {
		options.TailLines = defaultTailLines
	}
	if options.MaxLogBytes <= 0 {
		options.MaxLogBytes = defaultMaxLogBytes
	}

	pod, err := c.client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return model.DiagnoseRequest{}, fmt.Errorf("get pod %s/%s: %w", namespace, podName, err)
	}
	request := model.DiagnoseRequest{Pod: fromPod(pod), Events: []model.Event{}, Logs: []model.ContainerLog{}}
	events, err := c.client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("involvedObject.name", pod.Name).String(),
	})
	if err != nil {
		return model.DiagnoseRequest{}, fmt.Errorf("get events for pod %s/%s: %w", namespace, podName, err)
	}
	request.Events = make([]model.Event, 0, len(events.Items))
	for _, event := range events.Items {
		request.Events = append(request.Events, fromEvent(event))
	}

	for _, container := range append(append([]corev1.Container{}, pod.Spec.InitContainers...), pod.Spec.Containers...) {
		logText, err := c.containerLogs(ctx, namespace, pod.Name, container.Name, options)
		if err != nil {
			request.CollectionErrors = append(request.CollectionErrors, fmt.Sprintf("logs for container %s: %v", container.Name, err))
			continue
		}
		if strings.TrimSpace(logText) == "" {
			continue
		}
		request.Logs = append(request.Logs, model.ContainerLog{Container: container.Name, Previous: options.PreviousLogs, Text: logText})
	}
	return request, nil
}

func (c *Collector) containerLogs(ctx context.Context, namespace, podName, containerName string, options Options) (string, error) {
	stream, err := c.client.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		TailLines: &options.TailLines,
		Previous:  options.PreviousLogs,
	}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()
	data, err := io.ReadAll(io.LimitReader(stream, options.MaxLogBytes))
	return string(data), err
}

func fromPod(pod *corev1.Pod) model.Pod {
	result := model.Pod{
		APIVersion: "v1",
		Kind:       "Pod",
		Metadata: model.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
			UID:       string(pod.UID),
		},
		Spec: model.PodSpec{
			NodeName:           pod.Spec.NodeName,
			RestartPolicy:      string(pod.Spec.RestartPolicy),
			Containers:         fromContainers(pod.Spec.Containers),
			InitContainers:     fromContainers(pod.Spec.InitContainers),
			ActiveDeadlineSecs: pod.Spec.ActiveDeadlineSeconds,
		},
		Status: model.PodStatus{
			Phase:   string(pod.Status.Phase),
			Reason:  pod.Status.Reason,
			Message: pod.Status.Message,
		},
	}
	if pod.Status.StartTime != nil && !pod.Status.StartTime.IsZero() {
		start := pod.Status.StartTime.Time
		result.Status.StartTime = &start
	}
	for _, condition := range pod.Status.Conditions {
		result.Status.Conditions = append(result.Status.Conditions, model.PodCondition{
			Type:               string(condition.Type),
			Status:             string(condition.Status),
			Reason:             condition.Reason,
			Message:            condition.Message,
			LastTransitionTime: condition.LastTransitionTime.Time,
		})
	}
	result.Status.ContainerStatuses = fromContainerStatuses(pod.Status.ContainerStatuses)
	result.Status.InitContainerStatuses = fromContainerStatuses(pod.Status.InitContainerStatuses)
	return result
}

func fromContainers(containers []corev1.Container) []model.Container {
	result := make([]model.Container, 0, len(containers))
	for _, container := range containers {
		result = append(result, model.Container{
			Name:      container.Name,
			Image:     container.Image,
			Command:   container.Command,
			Args:      container.Args,
			Resources: fromResources(container.Resources),
		})
	}
	return result
}

func fromResources(resources corev1.ResourceRequirements) model.ResourceRequirements {
	result := model.ResourceRequirements{Requests: map[string]string{}, Limits: map[string]string{}}
	for name, quantity := range resources.Requests {
		result.Requests[string(name)] = quantity.String()
	}
	for name, quantity := range resources.Limits {
		result.Limits[string(name)] = quantity.String()
	}
	return result
}

func fromContainerStatuses(statuses []corev1.ContainerStatus) []model.ContainerStatus {
	result := make([]model.ContainerStatus, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, model.ContainerStatus{
			Name:         status.Name,
			Ready:        status.Ready,
			RestartCount: status.RestartCount,
			State:        fromContainerState(status.State),
			LastState:    fromContainerState(status.LastTerminationState),
		})
	}
	return result
}

func fromContainerState(state corev1.ContainerState) model.ContainerState {
	result := model.ContainerState{}
	if state.Waiting != nil {
		result.Waiting = &model.WaitingState{Reason: state.Waiting.Reason, Message: state.Waiting.Message}
	}
	if state.Running != nil {
		result.Running = &model.RunningState{StartedAt: timestampPointer(state.Running.StartedAt)}
	}
	if state.Terminated != nil {
		result.Terminated = &model.TerminatedState{
			ExitCode:   state.Terminated.ExitCode,
			Signal:     state.Terminated.Signal,
			Reason:     state.Terminated.Reason,
			Message:    state.Terminated.Message,
			StartedAt:  timestampPointer(state.Terminated.StartedAt),
			FinishedAt: timestampPointer(state.Terminated.FinishedAt),
		}
	}
	return result
}

func fromEvent(event corev1.Event) model.Event {
	first := event.FirstTimestamp.Time
	last := event.LastTimestamp.Time
	if first.IsZero() {
		first = event.CreationTimestamp.Time
	}
	if last.IsZero() {
		last = first
	}
	return model.Event{
		Type:      event.Type,
		Reason:    event.Reason,
		Message:   event.Message,
		Count:     event.Count,
		FirstTime: formatTime(first),
		LastTime:  formatTime(last),
		Source:    event.Source.Component,
	}
}

func timestampPointer(timestamp metav1.Time) *time.Time {
	if timestamp.IsZero() {
		return nil
	}
	value := timestamp.Time
	return &value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
