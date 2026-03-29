package controller

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	nodev1 "k8s.io/api/node/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	volundv1 "github.com/ai-volund/volund-operator/api/v1alpha1"
)

// resolveEnvtestAssets returns the path to envtest binaries.
// It checks KUBEBUILDER_ASSETS first, then tries setup-envtest, and
// falls back to the default kubebuilder location.
func resolveEnvtestAssets() string {
	if p := os.Getenv("KUBEBUILDER_ASSETS"); p != "" {
		return p
	}
	// Try setup-envtest.
	cmd := exec.Command("go", "run",
		"sigs.k8s.io/controller-runtime/tools/setup-envtest@latest",
		"use", "-p", "path")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		if p := strings.TrimSpace(out.String()); p != "" {
			return p
		}
	}
	return "/usr/local/kubebuilder/bin"
}

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	scheme    *runtime.Scheme
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		BinaryAssetsDirectory: resolveEnvtestAssets(),
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme = runtime.NewScheme()
	Expect(appsv1.AddToScheme(scheme)).To(Succeed())
	Expect(corev1.AddToScheme(scheme)).To(Succeed())
	Expect(networkingv1.AddToScheme(scheme)).To(Succeed())
	Expect(nodev1.AddToScheme(scheme)).To(Succeed())
	Expect(volundv1.AddToScheme(scheme)).To(Succeed())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start controller manager with all controllers.
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	err = (&AgentWarmPoolReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&AgentInstanceReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&AgentProfileReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&SkillReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&SandboxWarmPoolReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	err = (&SandboxClaimReconciler{Client: mgr.GetClient(), Scheme: mgr.GetScheme()}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	// Create the gVisor RuntimeClass so envtest's API server accepts sandbox pods.
	rc := &nodev1.RuntimeClass{
		ObjectMeta: metav1.ObjectMeta{Name: "gvisor"},
		Handler:    "runsc",
	}
	Expect(k8sClient.Create(context.Background(), rc)).To(Succeed())

	go func() {
		defer GinkgoRecover()
		err := mgr.Start(ctx)
		Expect(err).NotTo(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})
