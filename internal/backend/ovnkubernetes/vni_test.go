package ovnkubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

var _ = Describe("VNIAllocator", func() {
	const (
		vniMin = 4096
		vniMax = 16777215
		and    = "test-and"
	)

	var allocator *VNIAllocator

	BeforeEach(func() {
		allocator = NewVNIAllocator()
	})

	Describe("AllocateSubnetVNIs", func() {

		Context("for a Public subnet", func() {
			It("returns both MACVRF and IPVRF VNIs in the valid range", func() {
				vnis, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				Expect(vnis.MACVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.MACVRF).To(BeNumerically("<=", vniMax))
				Expect(vnis.IPVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.IPVRF).To(BeNumerically("<=", vniMax))
			})

			It("assigns distinct MACVRF and IPVRF VNIs for the same subnet", func() {
				vnis, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				Expect(vnis.MACVRF).NotTo(Equal(vnis.IPVRF))
			})
		})

		Context("for a Private subnet", func() {
			It("returns both MACVRF and IPVRF VNIs", func() {
				vnis, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePrivate)
				Expect(err).NotTo(HaveOccurred())
				Expect(vnis.MACVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.IPVRF).To(BeNumerically(">=", vniMin))
			})
		})

		Context("for an Isolated subnet", func() {
			It("returns only a MACVRF VNI (IPVRF is zero)", func() {
				vnis, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypeIsolated)
				Expect(err).NotTo(HaveOccurred())
				Expect(vnis.MACVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.IPVRF).To(Equal(0))
			})
		})

		Context("for a VPNOnly subnet", func() {
			It("returns both MACVRF and IPVRF VNIs", func() {
				vnis, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypeVPNOnly)
				Expect(err).NotTo(HaveOccurred())
				Expect(vnis.MACVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.IPVRF).To(BeNumerically(">=", vniMin))
				Expect(vnis.MACVRF).NotTo(Equal(vnis.IPVRF))
			})
		})

		Context("idempotency", func() {
			It("returns the same VNIs when called twice for the same subnet", func() {
				v1, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				v2, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				Expect(v1).To(Equal(v2))
			})
		})

		Context("uniqueness", func() {
			It("assigns different VNIs to different subnets", func() {
				va, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				vb, err := allocator.AllocateSubnetVNIs(and, "subnet-b", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				Expect(va.MACVRF).NotTo(Equal(vb.MACVRF))
				Expect(va.IPVRF).NotTo(Equal(vb.IPVRF))
			})

			It("assigns different VNIs to the same subnet name in different ANDs", func() {
				va, err := allocator.AllocateSubnetVNIs("and-a", "subnet-x", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				vb, err := allocator.AllocateSubnetVNIs("and-b", "subnet-x", v1beta1.SubnetTypePublic)
				Expect(err).NotTo(HaveOccurred())
				Expect(va.MACVRF).NotTo(Equal(vb.MACVRF))
			})
		})

	})

	Describe("ReleaseSubnetVNIs", func() {
		It("does not panic when releasing an allocated subnet", func() {
			_, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
			Expect(err).NotTo(HaveOccurred())
			Expect(func() {
				allocator.ReleaseSubnetVNIs(and, "subnet-a")
			}).NotTo(Panic())
		})

		It("does not panic when releasing a subnet that was never allocated", func() {
			Expect(func() {
				allocator.ReleaseSubnetVNIs(and, "nonexistent")
			}).NotTo(Panic())
		})

		It("allows re-allocation after release", func() {
			_, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
			Expect(err).NotTo(HaveOccurred())

			allocator.ReleaseSubnetVNIs(and, "subnet-a")

			// The bitmap is round-robin, so the re-allocated VNIs are not
			// required to be the ones just freed — only to be a fresh,
			// valid, non-overlapping pair. That the freed slots really do
			// return to the pool is pinned down by the idAllocator spec
			// below, where the pool is small enough to make it observable.
			v2, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
			Expect(err).NotTo(HaveOccurred())
			Expect(v2.MACVRF).To(BeNumerically(">=", vniMin))
			Expect(v2.IPVRF).To(BeNumerically(">=", vniMin))
			Expect(v2.MACVRF).NotTo(Equal(v2.IPVRF))
		})
	})

})

var _ = Describe("idAllocator", func() {
	const offset = 4096

	// A two-slot pool makes exhaustion and slot reuse directly observable;
	// the real VNI pool is 2^24 wide, so neither is reachable there.
	It("returns a released ID to the pool", func() {
		a := newIDAllocator("tiny", 2, offset)

		first, err := a.AllocateID("a")
		Expect(err).NotTo(HaveOccurred())
		second, err := a.AllocateID("b")
		Expect(err).NotTo(HaveOccurred())
		Expect(first).NotTo(Equal(second))

		_, err = a.AllocateID("c")
		Expect(err).To(MatchError(ContainSubstring("pool exhausted")))

		a.ReleaseID("a")

		// Only one slot is free, so the allocator has no choice but to
		// hand back exactly the ID that was released.
		reused, err := a.AllocateID("c")
		Expect(err).NotTo(HaveOccurred())
		Expect(reused).To(Equal(first))
	})

	It("rejects reserving an ID that another key already holds", func() {
		a := newIDAllocator("tiny", 2, offset)

		Expect(a.ReserveID("a", offset)).To(Succeed())
		// Reserving the same ID for the same key stays idempotent.
		Expect(a.ReserveID("a", offset)).To(Succeed())

		Expect(a.ReserveID("b", offset)).To(MatchError(ContainSubstring("already reserved")))
		Expect(a.ReserveID("a", offset+1)).To(MatchError(ContainSubstring("already allocated")))
	})
})
