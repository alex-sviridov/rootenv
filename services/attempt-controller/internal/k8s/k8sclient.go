package k8s

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// LabEnvironmentGVR identifies the cluster-scoped LabEnvironment custom
// resource (shortname "labenv") defined by services/labenv-operator.
var LabEnvironmentGVR = schema.GroupVersionResource{
	Group:    "lab.rootenv.io",
	Version:  "v1alpha1",
	Resource: "labenvironments",
}

// newK8sClient returns a dynamic client for accessing LabEnvironment custom
// resources, using in-cluster config when available and falling back to the
// local kubeconfig for development.
func NewClient() (dynamic.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		loadRules := clientcmd.NewDefaultClientConfigLoadingRules()
		cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadRules, nil).ClientConfig()
		if err != nil {
			return nil, fmt.Errorf("k8s config: %w", err)
		}
	}
	return dynamic.NewForConfig(cfg)
}
