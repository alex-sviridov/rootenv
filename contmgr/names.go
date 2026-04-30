package main

func netpolName(userID, attemptID string) string {
	return userID + "-" + attemptID + "-netpol"
}

func podName(userID, attemptID, assetName string) string {
	return userID + "-" + attemptID + "-" + assetName
}

func svcName(userID, attemptID, assetName string) string {
	return userID + "-" + attemptID + "-" + assetName + "-svc"
}

func svcDNS(svc, namespace string) string {
	return svc + "." + namespace + ".svc.cluster.local"
}
