package upstream

import (
	"context"
	"log"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"

	"github.com/alex-sviridov/rootenv/services/attempt-controller/internal/k8s"
)

const resyncPeriod = 5 * time.Minute

// Run lists all existing LabEnvironment CRs, reconciles each, then watches for
// changes and reconciles on every Add/Update/Delete event until ctx is cancelled.
// It is intended to run as a goroutine.
func (r *Reconciler) Run(ctx context.Context, dyn dynamic.Interface) {
	lw := &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			return dyn.Resource(k8s.LabEnvironmentGVR).List(ctx, opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			return dyn.Resource(k8s.LabEnvironmentGVR).Watch(ctx, opts)
		},
	}

	_, informer := cache.NewInformerWithOptions(cache.InformerOptions{
		ListerWatcher: lw,
		ObjectType:    &unstructured.Unstructured{},
		ResyncPeriod:  resyncPeriod,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				r.ReconcileLabEnv(ctx, u)
			},
			UpdateFunc: func(_, newObj any) {
				u, ok := newObj.(*unstructured.Unstructured)
				if !ok {
					return
				}
				r.ReconcileLabEnv(ctx, u)
			},
			DeleteFunc: func(obj any) {
				u, ok := obj.(*unstructured.Unstructured)
				if !ok {
					// tombstone — extract the object
					if d, ok := obj.(cache.DeletedFinalStateUnknown); ok {
						u, ok = d.Obj.(*unstructured.Unstructured)
						if !ok {
							return
						}
					} else {
						return
					}
				}
				r.ReconcileDelete(ctx, u)
			},
		},
	})

	log.Println("upstream: starting LabEnvironment watcher")
	informer.Run(ctx.Done())
	log.Println("upstream: LabEnvironment watcher stopped")
}
