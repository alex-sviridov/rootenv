package main

func namespaceName(attemptID string) string { return "rootenv-lab-" + attemptID }

func podName(assetName string) string { return assetName }

func svcName(assetName string) string { return assetName + "-svc" }

func svcDNS(svc, namespace string) string {
	return svc + "." + namespace + ".svc.cluster.local"
}
