package v1beta1

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

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

const (
	timeout  = 10 * time.Second
	interval = 250 * time.Millisecond
)

func TestAdministrativeNetworkDomainAPI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AdministrativeNetworkDomain API Suite")
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

var _ = Describe("AdministrativeNetworkDomain", func() {
	var created *AdministrativeNetworkDomain

	AfterEach(func() {
		if created != nil {
			_ = k8sClient.Delete(ctx, created)
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(created), &AdministrativeNetworkDomain{})
			}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
			created = nil
		}
	})

	It("accepts a valid AND with Public, Private, and Isolated subnets", func() {
		created = &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-valid"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{Name: "web", CIDRs: []CIDR{"10.0.1.0/24"}, Type: SubnetTypePublic},
					{Name: "app", CIDRs: []CIDR{"10.0.10.0/24"}, Type: SubnetTypePrivate},
					{Name: "db", CIDRs: []CIDR{"10.0.20.0/24"}, Type: SubnetTypeIsolated},
				},
			},
		}
		Expect(k8sClient.Create(ctx, created)).To(Succeed())
	})

	It("accepts dual-stack CIDRs on a subnet", func() {
		created = &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-dualstack"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{
						Name:  "web",
						CIDRs: []CIDR{"10.0.1.0/24", "fd00:1::/64"},
						Type:  SubnetTypePublic,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, created)).To(Succeed())
	})

	It("rejects a CIDR that is not a masked network address", func() {
		and := &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-host-bit"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{Name: "web", CIDRs: []CIDR{"10.0.1.1/24"}, Type: SubnetTypePublic},
				},
			},
		}
		err := k8sClient.Create(ctx, and)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})

	It("rejects two CIDRs from the same address family", func() {
		and := &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-two-v4"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{
						Name:  "web",
						CIDRs: []CIDR{"10.0.1.0/24", "10.0.2.0/24"},
						Type:  SubnetTypePublic,
					},
				},
			},
		}
		err := k8sClient.Create(ctx, and)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})

	It("rejects a combined AND+subnet name longer than 63 characters", func() {
		and := &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{Name: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CIDRs: []CIDR{"10.0.1.0/24"}, Type: SubnetTypePublic},
				},
			},
		}
		err := k8sClient.Create(ctx, and)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})

	It("rejects mutating an immutable subnet field", func() {
		created = &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-immutable"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{
					{Name: "web", CIDRs: []CIDR{"10.0.1.0/24"}, Type: SubnetTypePublic},
				},
			},
		}
		Expect(k8sClient.Create(ctx, created)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(created), created)).To(Succeed())
		created.Spec.Subnets[0].Type = SubnetTypePrivate
		err := k8sClient.Update(ctx, created)
		Expect(err).To(HaveOccurred())
		Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
	})
})
