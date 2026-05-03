package main

import "testing"

func TestResolveImage(t *testing.T) {
	cases := []struct {
		image    string
		registry string
		want     string
	}{
		// no registry set — always pass through
		{"rootenv-ubuntu-sshd", "", "rootenv-ubuntu-sshd"},
		{"hermsi/alpine-sshd", "", "hermsi/alpine-sshd"},
		{"docker.io/library/foo", "", "docker.io/library/foo"},

		// bare name (no slash) — prepend registry
		{"rootenv-ubuntu-sshd", "k3d-rootenv-registry:5111", "k3d-rootenv-registry:5111/rootenv-ubuntu-sshd"},

		// explicit registry host (contains '.') — leave untouched
		{"docker.io/library/rootenv-ubuntu-sshd", "k3d-rootenv-registry:5111", "docker.io/library/rootenv-ubuntu-sshd"},
		{"ghcr.io/foo/bar", "k3d-rootenv-registry:5111", "ghcr.io/foo/bar"},

		// user/image with no registry host — contains '/' but first segment has no '.' or ':'
		// These stay untouched (pulled from Docker Hub directly)
		{"hermsi/alpine-sshd", "k3d-rootenv-registry:5111", "hermsi/alpine-sshd"},
		{"library/ubuntu", "k3d-rootenv-registry:5111", "library/ubuntu"},
	}
	for _, tc := range cases {
		got := resolveImage(tc.image, tc.registry)
		if got != tc.want {
			t.Errorf("resolveImage(%q, %q) = %q, want %q", tc.image, tc.registry, got, tc.want)
		}
	}
}
