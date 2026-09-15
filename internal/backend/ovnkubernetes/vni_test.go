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
			v1, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
			Expect(err).NotTo(HaveOccurred())

			allocator.ReleaseSubnetVNIs(and, "subnet-a")

			v2, err := allocator.AllocateSubnetVNIs(and, "subnet-a", v1beta1.SubnetTypePublic)
			Expect(err).NotTo(HaveOccurred())

			// After release and re-alloc the VNIs may differ; just verify they are valid.
			Expect(v2.MACVRF).To(BeNumerically(">=", vniMin))
			_ = v1
		})
	})

})
