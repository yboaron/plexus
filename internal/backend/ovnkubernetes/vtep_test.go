package ovnkubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

var _ = Describe("vtep.go", func() {
	Describe("cidrsEqual", func() {
		It("returns true for identical CIDR lists", func() {
			a := []vtepv1.CIDR{"172.18.0.0/16", "fd00::/64"}
			b := []vtepv1.CIDR{"172.18.0.0/16", "fd00::/64"}
			Expect(cidrsEqual(a, b)).To(BeTrue())
		})

		It("ignores ordering", func() {
			a := []vtepv1.CIDR{"172.18.0.0/16", "fd00::/64"}
			b := []vtepv1.CIDR{"fd00::/64", "172.18.0.0/16"}
			Expect(cidrsEqual(a, b)).To(BeTrue())
			Expect(cidrsEqual(b, a)).To(BeTrue())
		})

		It("does not mutate its arguments", func() {
			a := []vtepv1.CIDR{"172.18.0.0/16", "fd00::/64"}
			b := []vtepv1.CIDR{"fd00::/64", "172.18.0.0/16"}
			Expect(cidrsEqual(a, b)).To(BeTrue())
			Expect(a).To(Equal([]vtepv1.CIDR{"172.18.0.0/16", "fd00::/64"}))
			Expect(b).To(Equal([]vtepv1.CIDR{"fd00::/64", "172.18.0.0/16"}))
		})

		It("returns true for two empty lists", func() {
			Expect(cidrsEqual(nil, []vtepv1.CIDR{})).To(BeTrue())
		})

		It("returns false for different length or values", func() {
			Expect(cidrsEqual([]vtepv1.CIDR{"172.18.0.0/16"}, []vtepv1.CIDR{"172.18.0.0/16", "10.0.0.0/8"})).To(BeFalse())
			Expect(cidrsEqual([]vtepv1.CIDR{"172.18.0.0/16"}, []vtepv1.CIDR{"10.0.0.0/8"})).To(BeFalse())
		})

		It("distinguishes lists that differ only in duplicate counts", func() {
			a := []vtepv1.CIDR{"172.18.0.0/16", "172.18.0.0/16"}
			b := []vtepv1.CIDR{"172.18.0.0/16", "10.0.0.0/8"}
			Expect(cidrsEqual(a, b)).To(BeFalse())
			Expect(cidrsEqual(b, a)).To(BeFalse())
		})
	})

	Describe("VTEP Management", func() {
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

		It("creates VTEP with CIDRs from OVNKubernetesConfig", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())
			Expect(vtep.Spec.CIDRs).To(Equal([]vtepv1.CIDR{"172.18.0.0/16"}))
			Expect(vtep.Spec.Mode).To(Equal(vtepv1.VTEPModeUnmanaged))
			Expect(vtep.Labels).To(HaveKeyWithValue(labelManagedBy, "plexus"))
		})

		It("is idempotent when VTEP already exists with correct spec", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))

			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			vtep1, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())

			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())
			vtep2, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())

			Expect(vtep2.ResourceVersion).To(Equal(vtep1.ResourceVersion), "VTEP should not be updated when spec is correct")
		})

		It("repairs drifted VTEP CIDRs", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())
			vtep.Spec.CIDRs = []vtepv1.CIDR{"192.168.0.0/16"} // drift
			Expect(k8sClient.Update(ctx, vtep)).To(Succeed())

			_, err = b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err = getVTEP()
			Expect(err).NotTo(HaveOccurred())
			Expect(vtep.Spec.CIDRs).To(Equal([]vtepv1.CIDR{"172.18.0.0/16"}), "VTEP CIDRs should be repaired")
		})

		It("creates VTEP even when AND has only Isolated subnets", func() {
			b := newBackend()
			and := makeAND(uniqueName("and"), makeSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated))
			_, err := b.Reconcile(ctx, and)
			Expect(err).NotTo(HaveOccurred())

			vtep, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())
			Expect(vtep.Spec.CIDRs).To(Equal([]vtepv1.CIDR{"172.18.0.0/16"}))
		})

		It("shares the VTEP across multiple ANDs", func() {
			b := newBackend()
			and1 := makeAND(uniqueName("and1"), makeSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			and2 := makeAND(uniqueName("and2"), makeSubnet("api", "10.0.2.0/24", andv1beta1.SubnetTypePublic))

			_, err := b.Reconcile(ctx, and1)
			Expect(err).NotTo(HaveOccurred())
			vtep1, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())

			_, err = b.Reconcile(ctx, and2)
			Expect(err).NotTo(HaveOccurred())
			vtep2, err := getVTEP()
			Expect(err).NotTo(HaveOccurred())

			// Same VTEP object, not updated
			Expect(vtep2.UID).To(Equal(vtep1.UID))
			Expect(vtep2.ResourceVersion).To(Equal(vtep1.ResourceVersion))
		})
	})
})
