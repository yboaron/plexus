package v1beta1

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestPlexusControllerConfigAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "PlexusControllerConfig API Suite")
}

var _ = BeforeSuite(func() {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		Skip("KUBEBUILDER_ASSETS not set — run 'make test' to include envtest")
	}

	ctrl.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.Background())

	root := findModuleRoot()
	Expect(root).NotTo(BeEmpty(), "could not locate module root (go.mod)")

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join(root, "config", "crd")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	scheme := k8sruntime.NewScheme()
	utilruntime.Must(AddToScheme(scheme))

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

var _ = Describe("PlexusControllerConfig", func() {
	var created *PlexusControllerConfig

	AfterEach(func() {
		if created != nil {
			_ = k8sClient.Delete(ctx, created)
			created = nil
		}
	})

	It("accepts a valid ovn-kubernetes configuration", func() {
		created = &PlexusControllerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "plexus"},
			Spec: PlexusControllerConfigSpec{
				Backend: "ovn-kubernetes",
				OVNKubernetes: &OVNKubernetesConfig{
					VTEPCIDRs: []CIDR{"172.18.0.0/16"},
					FRRConfigurationSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "frr"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, created)).To(Succeed())
	})

	It("rejects ovn-kubernetes backend without ovnKubernetes configuration", func() {
		cfg := &PlexusControllerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "plexus-missing-ovnk"},
			Spec: PlexusControllerConfigSpec{
				Backend: "ovn-kubernetes",
			},
		}
		err := k8sClient.Create(ctx, cfg)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})

	It("rejects an invalid VTEP CIDR", func() {
		cfg := &PlexusControllerConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "plexus-bad-cidr"},
			Spec: PlexusControllerConfigSpec{
				Backend: "ovn-kubernetes",
				OVNKubernetes: &OVNKubernetesConfig{
					VTEPCIDRs: []CIDR{"not-a-cidr"},
					FRRConfigurationSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "frr"},
					},
				},
			},
		}
		err := k8sClient.Create(ctx, cfg)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})
})
