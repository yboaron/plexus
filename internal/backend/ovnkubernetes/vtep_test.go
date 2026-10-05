package ovnkubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"
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
})
