package ovnkubernetes

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	udnv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

var _ = Describe("cudn.go", func() {
	Describe("domainRouteTarget", func() {
		It("returns a wildcard RT with a local admin in 1..65535", func() {
			rt := domainRouteTarget("production")
			Expect(rt).To(HavePrefix("*:"))
			var local uint32
			_, err := fmt.Sscanf(rt, "*:%d", &local)
			Expect(err).NotTo(HaveOccurred())
			Expect(local).To(BeNumerically(">=", 1))
			Expect(local).To(BeNumerically("<=", 65535))
		})

		It("is deterministic for the same AND name", func() {
			Expect(domainRouteTarget("production")).To(Equal(domainRouteTarget("production")))
		})

		It("differs across AND names", func() {
			Expect(domainRouteTarget("and-a")).NotTo(Equal(domainRouteTarget("and-b")))
		})
	})

	Describe("cudnName", func() {
		It("returns <and>-<subnet>", func() {
			and := testAND("prod")
			Expect(cudnName(and, testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))).To(Equal("prod-web"))
		})
	})

	Describe("buildCUDN", func() {
		var (
			b   *OVNKubernetesBackend
			and *andv1beta1.AdministrativeNetworkDomain
		)

		BeforeEach(func() {
			b = testBackend()
			and = testAND("prod")
		})

		It("builds a Layer2 EVPN CUDN with MACVRF and IPVRF for a Public subnet", func() {
			subnet := testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			cudn := b.buildCUDN(and, subnet, SubnetVNIs{MACVRF: 4096, IPVRF: 4097})

			Expect(cudn.Name).To(Equal("prod-web"))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelNetworkDomain, "prod"))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(cudn.Labels).To(HaveKeyWithValue(labelSubnetType, string(andv1beta1.SubnetTypePublic)))
			Expect(cudn.Spec.NamespaceSelector.MatchLabels).To(HaveKeyWithValue(labelSubnet, "web"))
			Expect(cudn.Spec.Network.Topology).To(Equal(udnv1.NetworkTopologyLayer2))
			Expect(cudn.Spec.Network.Transport).To(Equal(udnv1.TransportOptionEVPN))
			Expect(cudn.Spec.Network.Layer2.Role).To(Equal(udnv1.NetworkRolePrimary))
			Expect(cudn.Spec.Network.Layer2.Subnets).To(ConsistOf(udnv1.CIDR("10.0.1.0/24")))
			Expect(cudn.Spec.Network.EVPN.VTEP).To(Equal(vtepName))
			Expect(cudn.Spec.Network.EVPN.MACVRF.VNI).To(Equal(int32(4096)))
			Expect(cudn.Spec.Network.EVPN.IPVRF.VNI).To(Equal(int32(4097)))
			Expect(string(cudn.Spec.Network.EVPN.IPVRF.RouteTarget)).To(Equal(domainRouteTarget("prod")))
		})

		It("omits IPVRF for Isolated subnets even if an IPVRF VNI is supplied", func() {
			subnet := testSubnet("db", "10.0.20.0/24", andv1beta1.SubnetTypeIsolated)
			cudn := b.buildCUDN(and, subnet, SubnetVNIs{MACVRF: 4096, IPVRF: 4097})
			Expect(cudn.Spec.Network.EVPN.MACVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.IPVRF).To(BeNil())
		})

		It("includes IPVRF for VPNOnly subnets", func() {
			subnet := testSubnet("vpn", "10.0.30.0/24", andv1beta1.SubnetTypeVPNOnly)
			cudn := b.buildCUDN(and, subnet, SubnetVNIs{MACVRF: 4096, IPVRF: 4097})
			Expect(cudn.Spec.Network.EVPN.IPVRF).NotTo(BeNil())
			Expect(cudn.Spec.Network.EVPN.IPVRF.VNI).To(Equal(int32(4097)))
		})
	})
})
