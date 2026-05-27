package main

import "testing"

func TestNamespaceName(t *testing.T) {
	if got := namespaceName("attempt1"); got != "rootenv-lab-attempt1" {
		t.Fatalf("want rootenv-lab-attempt1, got %s", got)
	}
}

func TestPodName(t *testing.T) {
	if got := podName("server-0"); got != "server-0" {
		t.Fatalf("want server-0, got %s", got)
	}
}

func TestSvcName(t *testing.T) {
	if got := svcName("server-0"); got != "server-0-svc" {
		t.Fatalf("want server-0-svc, got %s", got)
	}
}

func TestSvcDNS(t *testing.T) {
	got := svcDNS("server-0-svc", "rootenv-lab-attempt1")
	want := "server-0-svc.rootenv-lab-attempt1.svc.cluster.local"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
