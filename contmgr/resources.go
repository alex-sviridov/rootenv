package main

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func dnsPorts() []networkingv1.NetworkPolicyPort {
	udp := corev1.ProtocolUDP
	tcp := corev1.ProtocolTCP
	port53 := intstr.FromInt32(53)
	return []networkingv1.NetworkPolicyPort{
		{Protocol: &udp, Port: &port53},
		{Protocol: &tcp, Port: &port53},
	}
}

// parseCPUMilli converts a CPU string like "1", "0.5", or "500m" to millicores.
func parseCPUMilli(cpu string) (int64, error) {
	if cpu == "" {
		return 0, nil
	}
	if strings.HasSuffix(cpu, "m") {
		var v int64
		if _, err := fmt.Sscanf(cpu[:len(cpu)-1], "%d", &v); err != nil {
			return 0, fmt.Errorf("unrecognized cpu millicore format: %q", cpu)
		}
		return v, nil
	}
	var v float64
	if _, err := fmt.Sscanf(cpu, "%f", &v); err != nil {
		return 0, fmt.Errorf("unrecognized cpu format: %q", cpu)
	}
	return int64(v * 1000), nil
}

// parseMemory converts strings like "512MB", "1GB", "256m" to bytes.
func parseMemory(mem string) (int64, error) {
	if mem == "" {
		return 0, nil
	}
	mem = strings.TrimSpace(mem)
	upper := strings.ToUpper(mem)
	units := map[string]int64{
		"GB": 1 << 30,
		"MB": 1 << 20,
		"KB": 1 << 10,
		"G":  1 << 30,
		"M":  1 << 20,
		"K":  1 << 10,
	}
	for suffix, mult := range units {
		if strings.HasSuffix(upper, suffix) {
			var v int64
			if _, err := fmt.Sscanf(mem[:len(mem)-len(suffix)], "%d", &v); err != nil {
				return 0, fmt.Errorf("unrecognized memory format: %q", mem)
			}
			return v * mult, nil
		}
	}
	var v int64
	if _, err := fmt.Sscanf(mem, "%d", &v); err != nil {
		return 0, fmt.Errorf("unrecognized memory format: %q", mem)
	}
	return v, nil
}
