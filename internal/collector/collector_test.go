package collector

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestCollectConvertsPodAndEvents(t *testing.T) {
	client := fake.NewSimpleClientset(
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "payments", UID: types.UID("pod-1")},
			Spec: corev1.PodSpec{
				NodeName:      "worker-1",
				RestartPolicy: corev1.RestartPolicyAlways,
			},
			Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{{Name: "api", Ready: false, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}}}},
		},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "api.1", Namespace: "payments"}, InvolvedObject: corev1.ObjectReference{Name: "api"}, Type: "Warning", Reason: "BackOff", Message: "backing off", Count: 3},
	)
	request, err := New(client).Collect(context.Background(), "payments", "api", Options{})
	if err != nil {
		t.Fatal(err)
	}
	if request.Pod.Metadata.UID != "pod-1" || request.Pod.Spec.NodeName != "worker-1" {
		t.Fatalf("pod conversion lost identity: %#v", request.Pod)
	}
	if len(request.Events) != 1 || request.Events[0].Reason != "BackOff" {
		t.Fatalf("event conversion failed: %#v", request.Events)
	}
	if request.Logs == nil || request.Events == nil {
		t.Fatal("collector must mark logs and events as collected, even when empty")
	}
}

func TestFromPodConvertsContainerResources(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api"}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api", Image: "example/api:v1", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resourceMustParse("500m")}}}}}}
	converted := fromPod(pod)
	if converted.Spec.Containers[0].Resources.Requests["cpu"] != "500m" {
		t.Fatalf("resource conversion lost CPU request: %#v", converted.Spec.Containers[0].Resources)
	}
}

func resourceMustParse(value string) resource.Quantity {
	return resource.MustParse(value)
}
