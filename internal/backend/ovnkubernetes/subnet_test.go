package ovnkubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

var _ = Describe("subnet.go", func() {
	Describe("namespaceName", func() {
		It("returns <and>-<subnet>", func() {
			Expect(namespaceName(testAND("prod"), testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))).
				To(Equal("prod-web"))
		})
	})

	// The label set is identical for every subnet type apart from
	// labelSubnetType, so the table doubles as a guard: a new SubnetType
	// that needs different labels will have to touch this test.
	DescribeTable("desiredLabels sets plexus and primary-UDN labels",
		func(subnetType andv1beta1.SubnetType) {
			and := testAND("prod")
			subnet := testSubnet("web", "10.0.1.0/24", subnetType)
			Expect(desiredLabels(and, subnet)).To(Equal(map[string]string{
				labelNetworkDomain: "prod",
				labelSubnet:        "web",
				labelSubnetType:    string(subnetType),
				labelPrimaryUDN:    "",
			}))
		},
		Entry("Public", andv1beta1.SubnetTypePublic),
		Entry("Private", andv1beta1.SubnetTypePrivate),
		Entry("Isolated", andv1beta1.SubnetTypeIsolated),
		Entry("VPNOnly", andv1beta1.SubnetTypeVPNOnly),
	)

	Describe("nodeSelectorAnnotationValue", func() {
		It("returns empty when no AZ or node selector is set", func() {
			Expect(nodeSelectorAnnotationValue(testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))).To(BeEmpty())
		})

		It("joins node-selector pairs in sorted key order", func() {
			subnet := testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			subnet.AvailabilityZone = &andv1beta1.AvailabilityZone{
				NodeSelector: map[string]string{
					"topology.kubernetes.io/zone": "rack-a",
					"node-role":                   "worker",
				},
			}
			Expect(nodeSelectorAnnotationValue(subnet)).To(Equal("node-role=worker,topology.kubernetes.io/zone=rack-a"))
		})
	})

	Describe("desiredAnnotations", func() {
		It("omits the node-selector annotation when unset", func() {
			Expect(desiredAnnotations(testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))).To(BeEmpty())
		})

		It("sets the node-selector annotation when an AZ node selector is present", func() {
			subnet := testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic)
			subnet.AvailabilityZone = &andv1beta1.AvailabilityZone{
				NodeSelector: map[string]string{"topology.kubernetes.io/zone": "rack-a"},
			}
			Expect(desiredAnnotations(subnet)).To(HaveKeyWithValue(annotationNodeSelector, "topology.kubernetes.io/zone=rack-a"))
		})
	})
})
