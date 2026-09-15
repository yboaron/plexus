package controller_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	rav1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1"
	udnv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1"
	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
	configv1beta1 "github.com/ovn-kubernetes/plexus/api/plexuscontrollerconfig/v1beta1"
	"github.com/ovn-kubernetes/plexus/internal/backend"
	"github.com/ovn-kubernetes/plexus/internal/controller"
	"github.com/ovn-kubernetes/plexus/internal/multicluster"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
	fakeBack  *fakeBackend
)

// fakeBackend is a test double for backend.Backend.
// All fields are mutex-protected so the reconciler goroutine and test
// goroutine can access them concurrently without a race.
type fakeBackend struct {
	mu              sync.Mutex
	reconcileResult backend.Result
	reconcileErr    error
	deleteErr       error
	deleteCalled    chan struct{}
	reconcileCount  int
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{deleteCalled: make(chan struct{}, 1)}
}

func (f *fakeBackend) setReconcile(result backend.Result, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconcileResult = result
	f.reconcileErr = err
}

// reset clears all state between tests.
func (f *fakeBackend) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconcileResult = backend.Result{}
	f.reconcileErr = nil
	f.deleteErr = nil
	f.reconcileCount = 0
	// drain channel
	select {
	case <-f.deleteCalled:
	default:
	}
}

func (f *fakeBackend) getReconcileCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reconcileCount
}

func (f *fakeBackend) setDeleteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteErr = err
}

func (f *fakeBackend) Reconcile(_ context.Context, _ *andv1beta1.AdministrativeNetworkDomain) (backend.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reconcileCount++
	return f.reconcileResult, f.reconcileErr
}

func (f *fakeBackend) Delete(_ context.Context, _ *andv1beta1.AdministrativeNetworkDomain) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case f.deleteCalled <- struct{}{}:
	default:
	}
	return f.deleteErr
}

func (f *fakeBackend) Name() string { return "fake" }

func TestController(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	// envtest binaries are downloaded via 'make test'.
	// Skip gracefully when they are not available so that
	// 'go test ./...' works for contributors without the binaries installed.
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		Skip("KUBEBUILDER_ASSETS not set — run 'make test' to download envtest binaries first")
	}

	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	root := findModuleRoot()
	Expect(root).NotTo(BeEmpty(), "could not locate module root (go.mod)")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join(root, "config", "crd"),
			filepath.Join(root, "test", "testdata", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(andv1beta1.AddToScheme(scheme))
	utilruntime.Must(configv1beta1.AddToScheme(scheme))
	utilruntime.Must(udnv1.AddToScheme(scheme))
	utilruntime.Must(rav1.AddToScheme(scheme))
	utilruntime.Must(vtepv1.AddToScheme(scheme))

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	fakeBack = newFakeBackend()

	inventory := multicluster.NewSecretInventory(multicluster.SecretInventoryOptions{
		HubClient: k8sClient,
		HubLabels: map[string]string{},
		Scheme:    scheme,
		Log:       ctrl.Log,
	})

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0", // disable metrics server in tests
		},
		HealthProbeBindAddress: "0", // disable health probes in tests
	})
	Expect(err).NotTo(HaveOccurred())

	err = (&controller.ANDReconciler{
		Client:    mgr.GetClient(),
		Backend:   fakeBack,
		Inventory: inventory,
	}).SetupWithManager(mgr)
	Expect(err).NotTo(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		Expect(mgr.Start(ctx)).To(Succeed())
	}()
})

var _ = AfterSuite(func() {
	if cancel != nil {
		cancel()
	}
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

func findModuleRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
