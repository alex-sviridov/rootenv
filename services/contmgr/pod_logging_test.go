package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// captureWarnLogs installs a slog handler that captures WARN lines into a
// buffer for the duration of the test, then restores the previous handler.
func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func podWithContainers(phase corev1.PodPhase, statuses []corev1.ContainerStatus, containers []corev1.Container) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "server-0", Namespace: "rootenv-lab-abc"},
		Spec:       corev1.PodSpec{Containers: containers},
		Status:     corev1.PodStatus{Phase: phase, ContainerStatuses: statuses},
	}
}

// --- logPodContainerStates ---

func TestLogPodContainerStates_ImagePullBackOff(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodPending,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "ImagePullBackOff",
					Message: "Back-off pulling image \"registry.example.com/mylab:latest\"",
				},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodContainerStates(pod)

	out := buf.String()
	for _, want := range []string{"ImagePullBackOff", "registry.example.com/mylab:latest", "server-0"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogPodContainerStates_ErrImagePull(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodPending,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Waiting: &corev1.ContainerStateWaiting{
					Reason:  "ErrImagePull",
					Message: "rpc error: code = NotFound",
				},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/missing:v1"}},
	)

	logPodContainerStates(pod)

	out := buf.String()
	for _, want := range []string{"ErrImagePull", "registry.example.com/missing:v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogPodContainerStates_TerminatedNonZero(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodFailed,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1,
					Reason:   "Error",
				},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodContainerStates(pod)

	out := buf.String()
	for _, want := range []string{"exit_code=1", "registry.example.com/mylab:latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogPodContainerStates_TerminatedZeroExitNoLog(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodSucceeded,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodContainerStates(pod)

	if buf.Len() != 0 {
		t.Errorf("expected no log output for exit code 0, got: %s", buf.String())
	}
}

func TestLogPodContainerStates_RunningNoLog(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodRunning,
		[]corev1.ContainerStatus{{
			Name:  "server-0",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodContainerStates(pod)

	if buf.Len() != 0 {
		t.Errorf("expected no log output for running container, got: %s", buf.String())
	}
}

// --- logPodEarlyExit ---

func TestLogPodEarlyExit_NonZeroExit(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodFailed,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 137,
					Reason:   "OOMKilled",
				},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodEarlyExit(pod)

	out := buf.String()
	for _, want := range []string{"exit_code=137", "OOMKilled", "registry.example.com/mylab:latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogPodEarlyExit_LastTerminationState(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodRunning,
		[]corev1.ContainerStatus{{
			Name:  "server-0",
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
			LastTerminationState: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{
					ExitCode: 1,
					Reason:   "Error",
				},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodEarlyExit(pod)

	out := buf.String()
	for _, want := range []string{"exit_code=1", "registry.example.com/mylab:latest"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in log output, got: %s", want, out)
		}
	}
}

func TestLogPodEarlyExit_ZeroExitNoLog(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodSucceeded,
		[]corev1.ContainerStatus{{
			Name: "server-0",
			State: corev1.ContainerState{
				Terminated: &corev1.ContainerStateTerminated{ExitCode: 0},
			},
		}},
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodEarlyExit(pod)

	if buf.Len() != 0 {
		t.Errorf("expected no log output for exit code 0, got: %s", buf.String())
	}
}

func TestLogPodEarlyExit_NoStatuses(t *testing.T) {
	buf := captureWarnLogs(t)

	pod := podWithContainers(corev1.PodPending, nil,
		[]corev1.Container{{Name: "server-0", Image: "registry.example.com/mylab:latest"}},
	)

	logPodEarlyExit(pod)

	if buf.Len() != 0 {
		t.Errorf("expected no log output with no statuses, got: %s", buf.String())
	}
}
