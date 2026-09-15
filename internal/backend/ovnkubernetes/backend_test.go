package ovnkubernetes

import (
	"fmt"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	rav1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1"
	udnv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1"
	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
	configv1beta1 "github.com/ovn-kubernetes/plexus/api/plexuscontrollerconfig/v1beta1"
	"github.com/ovn-kubernetes/plexus/internal/backend"
	"github.com/ovn-kubernetes/plexus/internal/multicluster"
)

var nameCounter int64

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, atomic.AddInt64(&nameCounter, 1))
}

// staticInventory is a ClusterInventory that returns a fixed cluster list.
type staticInventory struct {
	clusters []multicluster.ClusterInfo
}

func (s *staticInventory) MatchClusters(selector *metav1.LabelSelector) ([]multicluster.ClusterInfo, error) {
	if selector == nil {
		return append([]multicluster.ClusterInfo(nil), s.clusters...), nil
	}
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	var matched []multicluster.ClusterInfo
	for _, ci := range s.clusters {
		if sel.Matches(labels.Set(ci.Labels)) {
			matched = append(matched, ci)
		}
	}
	return matched, nil
}

func (s *staticInventory) GetCluster(name string) (multicluster.ClusterInfo, error) {
	for _, ci := range s.clusters {
		if ci.Name == name {
			return ci, nil
		}
	}
	return multicluster.ClusterInfo{}, fmt.Errorf("cluster %q not found", name)
}

func (s *staticInventory) AllClusters() ([]multicluster.ClusterInfo, error) {
	return append([]multicluster.ClusterInfo(nil), s.clusters...), nil
}

func defaultOVNConfig() *configv1beta1.OVNKubernetesConfig {
	return &configv1beta1.OVNKubernetesConfig{
		VTEPCIDRs: []configv1beta1.CIDR{"172.18.0.0/16"},
		FRRConfigurationSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "frr"},
		},
	}
}

func hubInventory() *staticInventory {
	return &staticInventory{
		clusters: []multicluster.ClusterInfo{{
			Name:   "hub",
			Client: k8sClient,
			IsHub:  true,
			Labels: map[string]string{"region": "us-east"},
		}},
	}
}

func newBackend() *OVNKubernetesBackend {
	return New(k8sClient, logr.Discard(), defaultOVNConfig(), hubInventory())
}

func makeAND(name string, subnets ...andv1beta1.Subnet) *andv1beta1.AdministrativeNetworkDomain {
	return &andv1beta1.AdministrativeNetworkDomain{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: andv1beta1.AdministrativeNetworkDomainSpec{
			Subnets: subnets,
		},
	}
}

func makeSubnet(name, cidr string, subnetType andv1beta1.SubnetType) andv1beta1.Subnet {
	return andv1beta1.Subnet{
		Name:  name,
		CIDRs: []andv1beta1.CIDR{andv1beta1.CIDR(cidr)},
		Type:  subnetType,
	}
}

func nsName(andName, subnetName string) string {
	return andName + "-" + subnetName
}

func getNamespace(name string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, ns)
	return ns, err
}

func getCUDN(name string) (*udnv1.ClusterUserDefinedNetwork, error) {
	cudn := &udnv1.ClusterUserDefinedNetwork{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, cudn)
	return cudn, err
}

func getRA(name string) (*rav1.RouteAdvertisements, error) {
	ra := &rav1.RouteAdvertisements{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: name}, ra)
	return ra, err
}

func getVTEP() (*vtepv1.VTEP, error) {
	vtep := &vtepv1.VTEP{}
	err := k8sClient.Get(ctx, client.ObjectKey{Name: vtepName}, vtep)
	return vtep, err
}

func markVTEPAccepted() {
	vtep, err := getVTEP()
	Expect(err).NotTo(HaveOccurred())
	apimeta.SetStatusCondition(&vtep.Status.Conditions, metav1.Condition{
		Type:    "Accepted",
		Status:  metav1.ConditionTrue,
		Reason:  "Accepted",
		Message: "accepted",
	})
	Expect(k8sClient.Status().Update(ctx, vtep)).To(Succeed())
}

func markCUDNReady(name string) {
	cudn, err := getCUDN(name)
	Expect(err).NotTo(HaveOccurred())
	apimeta.SetStatusCondition(&cudn.Status.Conditions, metav1.Condition{
		Type:    "NetworkCreated",
		Status:  metav1.ConditionTrue,
		Reason:  "NetworkCreated",
		Message: "created",
	})
	Expect(k8sClient.Status().Update(ctx, cudn)).To(Succeed())
}

func markRAAccepted(name string) {
	ra, err := getRA(name)
	Expect(err).NotTo(HaveOccurred())
	apimeta.SetStatusCondition(&ra.Status.Conditions, metav1.Condition{
		Type:    rav1.RouteAdvertisementsAccepted,
		Status:  metav1.ConditionTrue,
		Reason:  "Accepted",
		Message: "accepted",
	})
	Expect(k8sClient.Status().Update(ctx, ra)).To(Succeed())
}

func namespaceDeletedOrTerminating(name string) bool {
	ns, err := getNamespace(name)
	if apierrors.IsNotFound(err) {
		return true
	}
	return err == nil && ns.DeletionTimestamp != nil
}

func cleanupClusterResources() {
	_ = k8sClient.DeleteAllOf(ctx, &andv1beta1.AdministrativeNetworkDomain{})
	_ = k8sClient.DeleteAllOf(ctx, &udnv1.ClusterUserDefinedNetwork{})
	_ = k8sClient.DeleteAllOf(ctx, &rav1.RouteAdvertisements{})
	_ = k8sClient.DeleteAllOf(ctx, &vtepv1.VTEP{})

	var nsList corev1.NamespaceList
	if err := k8sClient.List(ctx, &nsList, client.HasLabels{labelNetworkDomain}); err == nil {
		for i := range nsList.Items {
			_ = k8sClient.Delete(ctx, &nsList.Items[i])
		}
	}
}

var _ = Describe("OVNKubernetesBackend", func() {
	It("returns ovn-kubernetes from Name", func() {
		b := New(nil, logr.Discard(), defaultOVNConfig(), &staticInventory{})
		Expect(b.Name()).To(Equal("ovn-kubernetes"))
	})
})

var _ = Describe("OVNKubernetesBackend resources", func() {
	BeforeEach(func() {
		if k8sClient == nil {
			Skip("KUBEBUILDER_ASSETS not set — run 'make test' to include envtest")
		}
		cleanupClusterResources()
	})

	AfterEach(func() {
		if k8sClient != nil {
			cleanupClusterResources()
		}
	})

	Describe("Namespace", func() {
		It("creates a namespace per subnet with plexus labels", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			ns, err := getNamespace(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).To(HaveKeyWithValue(labelNetworkDomain, and.Name))
			Expect(ns.Labels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(ns.Labels).To(HaveKeyWithValue(labelSubnetType, string(andv1beta1.SubnetTypePublic)))
			Expect(ns.Labels).To(HaveKeyWithValue(labelPrimaryUDN, ""))
		})

		It("sets the node-selector annotation from the availability zone", func() {
			b := newBackend()
			subnet := makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			subnet.AvailabilityZone = &andv1beta1.AvailabilityZone{
				ClusterSelector: metav1.LabelSelector{},
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "rack-a",
					"node-role":                   "worker",
				},
			}
			and := makeAND(uniqueName("and"), subnet)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			ns, err := getNamespace(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Annotations).To(HaveKeyWithValue(
				annotationNodeSelector,
				"node-role=worker,topology.kubernetes.io/zone=rack-a",
			))
		})

		It("repairs drifted namespace labels and removes a stale node-selector annotation", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePrivate))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			ns, err := getNamespace(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			ns.Labels[labelSubnetType] = "wrong"
			if ns.Annotations == nil {
				ns.Annotations = map[string]string{}
			}
			ns.Annotations[annotationNodeSelector] = "stale=true"
			Expect(k8sClient.Update(ctx, ns)).To(Succeed())

			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			ns, err = getNamespace(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(ns.Labels).To(HaveKeyWithValue(labelSubnetType, string(andv1beta1.SubnetTypePrivate)))
			_, hasAnnotation := ns.Annotations[annotationNodeSelector]
			Expect(hasAnnotation).To(BeFalse())
		})
	})

	Describe("ClusterUserDefinedNetwork", func() {
		It("creates a Layer2 EVPN CUDN for a Public subnet with MACVRF and IPVRF", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cudn, err := getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cudn.Labels).To(HaveKeyWithValue(labelNetworkDomain, and.Name))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(cudn.Spec.NamespaceSelector.MatchLabels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(cudn.Spec.Network.Topology).To(Equal(udnv1.NetworkTopologyLayer2))
			Expect(cudn.Spec.Network.Transport).To(Equal(udnv1.TransportOptionEVPN))
			Expect(cudn.Spec.Network.Layer2).NotTo(BeNil())
			Expect(cudn.Spec.Network.Layer2.Role).To(Equal(udnv1.NetworkRolePrimary))
			Expect(cudn.Spec.Network.Layer2.Subnets).To(ConsistOf(udnv1.CIDR("10.0.1.0/24")))
			Expect(cudn.Spec.Network.EVPN).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.VTEP).To(Equal(vtepName))
			Expect(cudn.Spec.Network.EVPN.MACVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.MACVRF.VNI).To(BeNumerically(">=", 4096))
			Expect(cudn.Spec.Network.EVPN.IPVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.IPVRF.VNI).To(BeNumerically(">=", 4096))
			Expect(cudn.Spec.Network.EVPN.IPVRF.VNI).NotTo(Equal(cudn.Spec.Network.EVPN.MACVRF.VNI))
			Expect(string(cudn.Spec.Network.EVPN.IPVRF.RouteTarget)).To(HavePrefix("*:"))
		})

		It("omits IPVRF on Isolated subnet CUDNs", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cudn, err := getCUDN(nsName(and.Name, "db"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cudn.Spec.Network.EVPN.MACVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.IPVRF).To(BeNil())
		})

		It("includes IPVRF on VPNOnly subnet CUDNs", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("vpn", "10.0.30.0/24", andv1beta1.SubnetTypeVPNOnly))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cudn, err := getCUDN(nsName(and.Name, "vpn"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cudn.Spec.Network.EVPN.IPVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.IPVRF.VNI).To(BeNumerically(">=", 4096))
		})

		It("repairs missing CUDN labels on a subsequent reconcile", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypeIsolated))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cudn, err := getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			cudn.Labels = map[string]string{}
			Expect(k8sClient.Update(ctx, cudn)).To(Succeed())

			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cudn, err = getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(cudn.Labels).To(HaveKeyWithValue(labelNetworkDomain, and.Name))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelSubnetType, string(andv1beta1.SubnetTypeIsolated)))
		})

		It("shares the same route target across non-Isolated subnets of one AND", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"),
				makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic),
				makeSubnet("app", "10.0.10.0/24", andv1beta1.SubnetTypePrivate),
			)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			web, err := getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			app, err := getCUDN(nsName(and.Name, "app"))
			Expect(err).NotTo(HaveOccurred())
			Expect(web.Spec.Network.EVPN.IPVRF.RouteTarget).To(Equal(app.Spec.Network.EVPN.IPVRF.RouteTarget))
		})

		It("preserves VNIs when a fresh backend reconciles an existing CUDN", func() {
			b1 := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b1.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			original, err := getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			macVNI := original.Spec.Network.EVPN.MACVRF.VNI
			ipVNI := original.Spec.Network.EVPN.IPVRF.VNI

			b2 := newBackend()
			_, err = b2.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			updated, err := getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
			Expect(updated.Spec.Network.EVPN.MACVRF.VNI).To(Equal(macVNI))
			Expect(updated.Spec.Network.EVPN.IPVRF.VNI).To(Equal(ipVNI))
		})
	})

	Describe("RouteAdvertisements", func() {
		It("creates a RouteAdvertisements selecting Public and Private CUDNs", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			ra, err := getRA(and.Name)
			Expect(err).NotTo(HaveOccurred())
			Expect(ra.Labels).To(HaveKeyWithValue(labelManagedBy, "plexus"))
			Expect(ra.Labels).To(HaveKeyWithValue(labelNetworkDomain, and.Name))
			Expect(ra.Spec.TargetVRF).To(Equal("auto"))
			Expect(ra.Spec.Advertisements).To(Equal([]rav1.AdvertisementType{rav1.PodNetwork}))
			Expect(ra.Spec.FRRConfigurationSelector.MatchLabels).To(HaveKeyWithValue("app", "frr"))
			Expect(ra.Spec.NetworkSelectors).To(HaveLen(1))
			sel := ra.Spec.NetworkSelectors[0].ClusterUserDefinedNetworkSelector
			Expect(sel).NotTo(BeNil())
			Expect(sel.NetworkSelector.MatchLabels).To(HaveKeyWithValue(labelNetworkDomain, and.Name))
			Expect(sel.NetworkSelector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
				Key:      labelSubnetType,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{string(andv1beta1.SubnetTypePublic), string(andv1beta1.SubnetTypePrivate)},
			}))
		})

		It("does not create RouteAdvertisements when every subnet is Isolated", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			_, err = getRA(and.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("deletes RouteAdvertisements after the last non-Isolated subnet is removed", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"),
				makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic),
				makeSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated),
			)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			_, err = getRA(and.Name)
			Expect(err).NotTo(HaveOccurred())

			and.Spec.Subnets = []andv1beta1.Subnet{makeSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated)}
			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			_, err = getRA(and.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})
	})

	Describe("VTEP", func() {
		It("creates the shared unmanaged VTEP with configured CIDRs", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())
			Expect(vtep.Labels).To(HaveKeyWithValue(labelManagedBy, "plexus"))
			Expect(vtep.Spec.Mode).To(Equal(vtepv1.VTEPModeUnmanaged))
			Expect(vtep.Spec.CIDRs).To(ConsistOf(vtepv1.CIDR("172.18.0.0/16")))
		})

		It("updates VTEP CIDRs when the backend config changes", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			cfg := defaultOVNConfig()
			cfg.VTEPCIDRs = []configv1beta1.CIDR{"10.100.0.0/16"}
			updated := New(k8sClient, logr.Discard(), cfg, hubInventory())
			_, err = updated.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())
			Expect(vtep.Spec.CIDRs).To(ConsistOf(vtepv1.CIDR("10.100.0.0/16")))
		})
	})

	Describe("status aggregation", func() {
		It("reports VTEPNotReady until the VTEP is accepted", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			result, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
			Expect(result.StatusReason).To(Equal("VTEPNotReady"))
		})

		It("reports SubnetsNotReady until CUDNs and RouteAdvertisements are accepted", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			markVTEPAccepted()
			result, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
			Expect(result.StatusReason).To(Equal("SubnetsNotReady"))
			Expect(result.StatusMessage).To(ContainSubstring("CUDN"))

			markCUDNReady(nsName(and.Name, "web"))
			result, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Requeue).To(BeTrue())
			Expect(result.StatusReason).To(Equal("SubnetsNotReady"))
			Expect(result.StatusMessage).To(ContainSubstring("RouteAdvertisements"))

			markRAAccepted(and.Name)
			result, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(backend.Result{}))
		})
	})

	Describe("garbage collection", func() {
		It("deletes Namespace and CUDN for a subnet removed from the AND spec", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"),
				makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic),
				makeSubnet("app", "10.0.10.0/24", andv1beta1.SubnetTypePrivate),
			)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			_, err = getCUDN(nsName(and.Name, "app"))
			Expect(err).NotTo(HaveOccurred())

			and.Spec.Subnets = []andv1beta1.Subnet{makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)}
			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			_, err = getCUDN(nsName(and.Name, "app"))
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			Expect(namespaceDeletedOrTerminating(nsName(and.Name, "app"))).To(BeTrue())

			_, err = getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("Delete", func() {
		It("removes AND-scoped CUDNs, RouteAdvertisements, namespaces, and the shared VTEP", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			Expect(k8sClient.Create(ctx, and.DeepCopy())).To(Succeed())
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			Expect(b.Delete(ctx, and)).To(Succeed())

			_, err = getCUDN(nsName(and.Name, "web"))
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			_, err = getRA(and.Name)
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			Expect(namespaceDeletedOrTerminating(nsName(and.Name, "web"))).To(BeTrue())
			_, err = getVTEP()
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		})

		It("keeps the shared VTEP while another AND still exists", func() {
			b := newBackend()
			andA := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			andB := makeAND(uniqueName("and"), makeSubnet("app", "10.0.10.0/24", andv1beta1.SubnetTypePrivate))
			Expect(k8sClient.Create(ctx, andA.DeepCopy())).To(Succeed())
			Expect(k8sClient.Create(ctx, andB.DeepCopy())).To(Succeed())

			_, err := b.Reconcile(ctx, andA)
			Expect(err).NotTo(HaveOccurred())
			_, err = b.Reconcile(ctx, andB)
			Expect(err).NotTo(HaveOccurred())

			Expect(b.Delete(ctx, andA)).To(Succeed())

			_, err = getVTEP()
			Expect(err).NotTo(HaveOccurred())
			_, err = getCUDN(nsName(andA.Name, "web"))
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			_, err = getCUDN(nsName(andB.Name, "app"))
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("cluster selector", func() {
		It("renders subnet resources when the hub matches the availability zone", func() {
			b := newBackend()
			subnet := makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			subnet.AvailabilityZone = &andv1beta1.AvailabilityZone{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"region": "us-east"},
				},
			}
			and := makeAND(uniqueName("and"), subnet)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			_, err = getCUDN(nsName(and.Name, "web"))
			Expect(err).NotTo(HaveOccurred())
		})

		It("skips subnet resources on clusters that do not match the availability zone", func() {
			b := newBackend()
			subnet := makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			subnet.AvailabilityZone = &andv1beta1.AvailabilityZone{
				ClusterSelector: metav1.LabelSelector{
					MatchLabels: map[string]string{"region": "us-west"},
				},
			}
			and := makeAND(uniqueName("and"), subnet)
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			_, err = getNamespace(nsName(and.Name, "web"))
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
			_, err = getCUDN(nsName(and.Name, "web"))
			Expect(apierrors.IsNotFound(err)).To(BeTrue())

			// Shared VTEP is still reconciled on every cluster in the inventory.
			_, err = getVTEP()
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
