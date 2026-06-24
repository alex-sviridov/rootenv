package exec

import (
	"net/http"
	"testing"

	"k8s.io/client-go/transport/spdy"
)

// TestKubeExecer_reuses_transport verifies that KubeExecer accepts a pre-built
// transport/upgrader pair and passes them through to each Exec call unchanged.
func TestKubeExecer_reuses_transport(t *testing.T) {
	sentinel := &http.Transport{}
	var upgrader spdy.Upgrader

	ke := &KubeExecer{
		host:      "https://apiserver:6443",
		transport: sentinel,
		upgrader:  upgrader,
	}

	execURL := podExecURL(ke.host, "ns", "pod")
	_, err := ke.newExecutor(execURL)
	if err != nil {
		t.Fatalf("newExecutor: %v", err)
	}
	if ke.transport != sentinel {
		t.Error("transport was replaced between calls")
	}
}

func TestKubeExecer_newExecutor_called_twice_same_transport(t *testing.T) {
	sentinel := &http.Transport{}
	ke := &KubeExecer{host: "https://apiserver:6443", transport: sentinel}

	url1 := podExecURL(ke.host, "ns", "pod-a")
	url2 := podExecURL(ke.host, "ns", "pod-b")
	if _, err := ke.newExecutor(url1); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if _, err := ke.newExecutor(url2); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if ke.transport != sentinel {
		t.Error("transport identity changed across calls")
	}
}

func TestPodExecURL(t *testing.T) {
	tests := []struct {
		name      string
		host      string
		namespace string
		podName   string
		want      string
	}{
		{
			name:      "basic",
			host:      "https://10.96.0.1:6443",
			namespace: "rootenv-lab-atm_abc",
			podName:   "workstation",
			want:      "https://10.96.0.1:6443/api/v1/namespaces/rootenv-lab-atm_abc/pods/workstation/exec?command=%2Fbin%2Fsh&stderr=true&stdin=true&stdout=true&tty=true",
		},
		{
			name:      "host with trailing slash stripped",
			host:      "https://apiserver:443/",
			namespace: "ns",
			podName:   "pod",
			want:      "https://apiserver:443/api/v1/namespaces/ns/pods/pod/exec?command=%2Fbin%2Fsh&stderr=true&stdin=true&stdout=true&tty=true",
		},
		{
			name:      "host with multiple trailing slashes",
			host:      "https://apiserver:443///",
			namespace: "ns",
			podName:   "pod",
			want:      "https://apiserver:443/api/v1/namespaces/ns/pods/pod/exec?command=%2Fbin%2Fsh&stderr=true&stdin=true&stdout=true&tty=true",
		},
		{
			name:      "namespace with underscores and hyphens",
			host:      "https://10.0.0.1:6443",
			namespace: "rootenv-lab-atm_xyz-123",
			podName:   "my-pod",
			want:      "https://10.0.0.1:6443/api/v1/namespaces/rootenv-lab-atm_xyz-123/pods/my-pod/exec?command=%2Fbin%2Fsh&stderr=true&stdin=true&stdout=true&tty=true",
		},
		{
			name:      "host with path prefix",
			host:      "https://apiserver:6443/k8s/cluster",
			namespace: "ns",
			podName:   "pod",
			want:      "https://apiserver:6443/k8s/cluster/api/v1/namespaces/ns/pods/pod/exec?command=%2Fbin%2Fsh&stderr=true&stdin=true&stdout=true&tty=true",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := podExecURL(tc.host, tc.namespace, tc.podName)
			if got.String() != tc.want {
				t.Errorf("podExecURL:\n got  %s\n want %s", got.String(), tc.want)
			}
		})
	}
}
