package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func main() {
	zapOpts := zap.Options{Development: os.Getenv("LOG_LEVEL") == "debug"}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))

	cfg, err := loadConfig()
	if err != nil {
		slog.Error("config error", "err", err)
		os.Exit(1)
	}

	ctx := ctrl.SetupSignalHandler()

	pb, err := authWithRetry(ctx, cfg)
	if err != nil {
		slog.Error("PocketBase auth failed", "err", err)
		os.Exit(1)
	}
	slog.Info("authenticated with PocketBase", "url", cfg.pbURL)

	k8sRaw, err := newK8sClient()
	if err != nil {
		slog.Error("k8s client error", "err", err)
		os.Exit(1)
	}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		slog.Error("scheme setup", "err", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: cfg.probeAddr,
		Metrics:                metricsserver.Options{BindAddress: cfg.metricsAddr},
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.Pod{}: {
					Label: labels.SelectorFromSet(labels.Set{
						"app.kubernetes.io/managed-by": "rootenv-contmgr",
					}),
				},
			},
		},
	})
	if err != nil {
		slog.Error("manager create", "err", err)
		os.Exit(1)
	}

	business := NewContmgr(pb, k8sRaw, cfg.infraNamespace, cfg.imagePullSecret, cfg.runtimeClass)

	if err := (&LabReconciler{
		contmgr:      business,
		pollInterval: cfg.pollInterval,
		cfg:          cfg,
	}).SetupWithManager(mgr); err != nil {
		slog.Error("lab reconciler setup", "err", err)
		os.Exit(1)
	}

	if err := (&PodStatusController{
		Client:  mgr.GetClient(),
		contmgr: business,
	}).SetupWithManager(mgr); err != nil {
		slog.Error("pod status controller setup", "err", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		slog.Error("add healthz", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		slog.Error("add readyz", "err", err)
		os.Exit(1)
	}

	slog.Info("contmgr starting", "infra_namespace", cfg.infraNamespace, "poll_interval", cfg.pollInterval)

	if err := mgr.Start(ctx); err != nil {
		slog.Error("manager exited", "err", err)
		os.Exit(1)
	}
}

const authBackoffCap = 60 * time.Second

func authWithRetry(ctx context.Context, cfg config) (*pbClient, error) {
	var err error
	for i := 0; ; i++ {
		var pb *pbClient
		pb, err = newPBClient(cfg.pbURL, cfg.pbEmail, cfg.pbPassword)
		if err == nil {
			return pb, nil
		}
		backoff := time.Duration(1<<i) * time.Second
		if backoff > authBackoffCap {
			backoff = authBackoffCap
		}
		slog.Warn("PocketBase auth attempt failed", "attempt", i+1, "backoff", backoff, "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
	}
}
