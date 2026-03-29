// Command operator is the Volund Kubernetes operator entrypoint.
// It manages AgentWarmPool and AgentInstance custom resources.
package main

import (
	"flag"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
	"github.com/ai-volund/volund-operator/internal/controller"
)

func main() {
	var metricsAddr string
	var probeAddr string
	var leaderElect bool

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "Metrics server bind address.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "Health probe bind address.")
	flag.BoolVar(&leaderElect, "leader-elect", false, "Enable leader election for high availability.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	logger := ctrl.Log.WithName("operator")

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add core scheme")
		os.Exit(1)
	}
	if err := appsv1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add apps scheme")
		os.Exit(1)
	}
	if err := volundv1.AddToScheme(scheme); err != nil {
		logger.Error(err, "add volund scheme")
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: server.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "volund-operator.volund.ai",
	})
	if err != nil {
		logger.Error(err, "create manager")
		os.Exit(1)
	}

	if err := (&controller.AgentWarmPoolReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup AgentWarmPool controller")
		os.Exit(1)
	}

	if err := (&controller.AgentInstanceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup AgentInstance controller")
		os.Exit(1)
	}

	if err := (&controller.AgentProfileReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup AgentProfile controller")
		os.Exit(1)
	}

	if err := (&controller.SkillReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		logger.Error(err, "setup Skill controller")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error(err, "add healthz check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error(err, "add readyz check")
		os.Exit(1)
	}

	logger.Info("starting operator")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "operator exited")
		os.Exit(1)
	}
}
