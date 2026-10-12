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

// Test constants for common subnet and CIDR values
const (
	testSubnetWeb    = "web"
	testSubnetDB     = "db"
	testCIDRWeb      = "10.0.1.0/24"
	testCIDRDB       = "10.0.2.0/24"
	testCIDRModified = "192.168.1.0/24"
	testCIDRIPv6     = "fd00::/64"
)

// makeTestAND creates an AdministrativeNetworkDomain with the given name and subnets.
// This reduces boilerplate in tests that need to create AND fixtures.
// Note: ResourceVersion is not set - it will be assigned by the API server on create.
func makeTestAND(name string, subnets ...Subnet) *AdministrativeNetworkDomain {
	return &AdministrativeNetworkDomain{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       AdministrativeNetworkDomainSpec{Subnets: subnets},
	}
}

// makeTestSubnet creates a Subnet with the given parameters.
func makeTestSubnet(name, cidr string, subnetType SubnetType) Subnet {
	return Subnet{
		Name:  name,
		CIDRs: []CIDR{CIDR(cidr)},
		Type:  subnetType,
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

	It("accepts an AND with no subnets", func() {
		// Subnets is optional and carries no MinItems: an empty AND is a
		// valid, if inert, domain. The controller reports it as
		// Ready=False/NoSubnets rather than the API server rejecting it.
		created = &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-no-subnets"},
			Spec:       AdministrativeNetworkDomainSpec{},
		}
		Expect(k8sClient.Create(ctx, created)).To(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(created), created)).To(Succeed())
		Expect(created.Spec.Subnets).To(BeEmpty())
	})

	It("accepts an AND with an explicitly empty subnet list", func() {
		created = &AdministrativeNetworkDomain{
			ObjectMeta: metav1.ObjectMeta{Name: "and-crd-empty-subnets"},
			Spec: AdministrativeNetworkDomainSpec{
				Subnets: []Subnet{},
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

	Describe("DeepCopy", func() {
		It("deep copies AdministrativeNetworkDomain without sharing slice references", func() {
			original := makeTestAND("test-and",
				makeTestSubnet(testSubnetWeb, testCIDRWeb, SubnetTypePublic),
				makeTestSubnet(testSubnetDB, testCIDRDB, SubnetTypePrivate),
			)
			copy := original.DeepCopy()

			Expect(copy).To(Equal(original), "copy should equal original")
			Expect(copy).NotTo(BeIdenticalTo(original), "copy should be a different object")

			// Mutate copy - original must remain unchanged
			copy.Spec.Subnets[0].Name = "modified"
			copy.Spec.Subnets[0].CIDRs[0] = CIDR(testCIDRModified)
			Expect(original.Spec.Subnets[0].Name).To(Equal(testSubnetWeb))
			Expect(original.Spec.Subnets[0].CIDRs[0]).To(Equal(CIDR(testCIDRWeb)))
		})

		It("deep copies nil slices as nil", func() {
			original := makeTestAND("test-nil-subnets")
			copy := original.DeepCopy()
			Expect(copy.Spec.Subnets).To(BeNil(), "nil Subnets should be preserved")
		})

		It("deep copies status conditions without sharing references", func() {
			original := makeTestAND("test-status")
			original.Status.Conditions = []metav1.Condition{
				{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "all good"},
			}
			copy := original.DeepCopy()

			copy.Status.Conditions[0].Status = metav1.ConditionFalse
			copy.Status.Conditions[0].Message = "modified"
			Expect(original.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue), "original status should not change")
			Expect(original.Status.Conditions[0].Message).To(Equal("all good"), "original message should not change")
		})

		It("deep copies AdministrativeNetworkDomainList", func() {
			original := &AdministrativeNetworkDomainList{
				Items: []AdministrativeNetworkDomain{
					*makeTestAND("and1"),
					*makeTestAND("and2"),
				},
			}
			copy := original.DeepCopy()

			Expect(copy).To(Equal(original), "copy should equal original list")
			copy.Items[0].Name = "modified"
			Expect(original.Items[0].Name).To(Equal("and1"), "original list item should not change")
		})

		It("deep copies with DeepCopyInto", func() {
			original := makeTestAND("test-copyinto", makeTestSubnet(testSubnetWeb, testCIDRWeb, SubnetTypePublic))
			var target AdministrativeNetworkDomain
			original.DeepCopyInto(&target)

			Expect(&target).To(Equal(original), "target should equal original")
			target.Spec.Subnets[0].Name = "modified"
			Expect(original.Spec.Subnets[0].Name).To(Equal(testSubnetWeb), "original should not change after modifying target")
		})

		It("deep copies with DeepCopyObject", func() {
			original := makeTestAND("test-copyobject")
			copyObj := original.DeepCopyObject()

			copyAND, ok := copyObj.(*AdministrativeNetworkDomain)
			Expect(ok).To(BeTrue(), "DeepCopyObject should return *AdministrativeNetworkDomain")
			Expect(copyAND).To(Equal(original))
		})

		It("deep copies Subnet with availability zone", func() {
			original := Subnet{
				Name:  testSubnetWeb,
				CIDRs: []CIDR{CIDR(testCIDRWeb), CIDR(testCIDRIPv6)},
				Type:  SubnetTypePublic,
				AvailabilityZone: &AvailabilityZone{
					ClusterSelector: metav1.LabelSelector{
						MatchLabels: map[string]string{"region": "us-east"},
					},
					NodeSelector: map[string]string{"zone": "a"},
				},
			}
			copy := original.DeepCopy()

			Expect(copy).To(Equal(&original), "copy should equal original subnet")
			copy.AvailabilityZone.NodeSelector["zone"] = "b"
			Expect(original.AvailabilityZone.NodeSelector["zone"]).To(Equal("a"), "original availability zone should not change")
		})
	})

	Describe("Additional Validation Edge Cases", func() {
		It("accepts an IPv6-only subnet", func() {
			and := makeTestAND("and-ipv6-only", makeTestSubnet(testSubnetWeb, testCIDRIPv6, SubnetTypePublic))
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND with IPv6-only subnet should succeed")
			created = and
		})

		It("accepts a combined name at exactly 63 characters", func() {
			// AND name (31 chars) + hyphen (1) + subnet name (31 chars) = 63 total
			and := makeTestAND("a123456789012345678901234567890",
				makeTestSubnet("b123456789012345678901234567890", testCIDRWeb, SubnetTypePublic),
			)
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND with 63-char combined name should succeed")
			created = and
		})

		It("rejects mutating subnet CIDRs", func() {
			and := makeTestAND("and-mutate-cidr", makeTestSubnet(testSubnetWeb, testCIDRWeb, SubnetTypePublic))
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND should succeed")
			created = and

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(and), and)).To(Succeed())
			and.Spec.Subnets[0].CIDRs = []CIDR{CIDR(testCIDRDB)}
			err := k8sClient.Update(ctx, and)
			Expect(err).To(HaveOccurred(), "mutating subnet CIDRs should fail")
			Expect(apierrors.IsInvalid(err) || apierrors.IsBadRequest(err)).To(BeTrue())
		})

		It("allows adding new subnets to existing AND", func() {
			and := makeTestAND("and-add-subnet", makeTestSubnet(testSubnetWeb, testCIDRWeb, SubnetTypePublic))
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND should succeed")
			created = and

			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(and), and)).To(Succeed())
			and.Spec.Subnets = append(and.Spec.Subnets,
				makeTestSubnet(testSubnetDB, testCIDRDB, SubnetTypePrivate),
			)
			Expect(k8sClient.Update(ctx, and)).To(Succeed(), "adding new subnet should succeed")
		})

		It("accepts VPNOnly subnet type", func() {
			and := makeTestAND("and-vpnonly", makeTestSubnet("vpn", "10.0.50.0/24", SubnetTypeVPNOnly))
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND with VPNOnly subnet should succeed")
			created = and
		})

		It("accepts subnet with availability zone", func() {
			subnet := makeTestSubnet(testSubnetWeb, testCIDRWeb, SubnetTypePublic)
			subnet.AvailabilityZone = &AvailabilityZone{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"region": "us-east"},
				},
				NodeSelector: map[string]string{"topology.kubernetes.io/zone": "rack-a"},
			}
			and := makeTestAND("and-with-az", subnet)
			Expect(k8sClient.Create(ctx, and)).To(Succeed(), "creating AND with availability zone should succeed")
			created = and
		})
	})
})
