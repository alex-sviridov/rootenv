package main

import "testing"

func TestPodName(t *testing.T) {
	if got := podName("user1", "attempt1", "server-0"); got != "user1-attempt1-server-0" {
		t.Fatalf("want user1-attempt1-server-0, got %s", got)
	}
}

func TestSvcName(t *testing.T) {
	if got := svcName("user1", "attempt1", "server-0"); got != "user1-attempt1-server-0-svc" {
		t.Fatalf("want user1-attempt1-server-0-svc, got %s", got)
	}
}

func TestNetpolName(t *testing.T) {
	if got := netpolName("user1", "attempt1"); got != "user1-attempt1-netpol" {
		t.Fatalf("want user1-attempt1-netpol, got %s", got)
	}
}

func TestSvcDNS(t *testing.T) {
	got := svcDNS("user1-attempt1-server-0-svc", "rootenv-users")
	want := "user1-attempt1-server-0-svc.rootenv-users.svc.cluster.local"
	if got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}
