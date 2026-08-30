package main

import (
	"reflect"
	"testing"
)

func TestNormalizeKubectlStylePodCommand(t *testing.T) {
	got, err := normalizeArgs([]string{"diagnose", "pod/api", "-n", "payments", "--watch", "--interval", "5s"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"diagnose", "--pod", "pod/api", "--namespace", "payments", "--watch", "--interval", "5s"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized args = %#v, want %#v", got, want)
	}
}

func TestNormalizeKubectlStylePodNameCommand(t *testing.T) {
	got, err := normalizeArgs([]string{"pod", "api", "--namespace=payments"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"diagnose", "--pod", "pod/api", "--namespace", "payments"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized args = %#v, want %#v", got, want)
	}
}

func TestNormalizeRejectsIncompleteNamespace(t *testing.T) {
	if _, err := normalizeArgs([]string{"pod/api", "-n"}); err == nil {
		t.Fatal("expected missing namespace error")
	}
}
