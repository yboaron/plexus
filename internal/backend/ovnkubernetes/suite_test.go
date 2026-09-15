package ovnkubernetes

import (
	"context"
	"os"
	"path/filepath"
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

	rav1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1"
	udnv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1"
	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
	configv1beta1 "github.com/ovn-kubernetes/plexus/api/plexuscontrollerconfig/v1beta1"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestOVNKubernetes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "OVNKubernetes Suite")
}

var _ = BeforeSuite(func() {
	ctx, cancel = context.WithCancel(context.Background())

	// VNIAllocator specs are pure unit tests and always run. Resource
	// specs that talk to the API server only run when envtest binaries
	// are present (make test).
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		return
	}

	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

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
