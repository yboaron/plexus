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

		It("returns false for different length or values", func() {
			Expect(cidrsEqual([]vtepv1.CIDR{"172.18.0.0/16"}, []vtepv1.CIDR{"172.18.0.0/16", "10.0.0.0/8"})).To(BeFalse())
			Expect(cidrsEqual([]vtepv1.CIDR{"172.18.0.0/16"}, []vtepv1.CIDR{"10.0.0.0/8"})).To(BeFalse())
		})
	})
})
