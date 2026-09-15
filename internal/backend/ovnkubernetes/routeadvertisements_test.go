package ovnkubernetes

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rav1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1"
	crdtypes "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/types"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

var _ = Describe("routeadvertisements.go", func() {
	Describe("raName", func() {
		It("uses the AND name", func() {
			Expect(raName(testAND("prod"))).To(Equal("prod"))
		})
	})

	Describe("buildRouteAdvertisements", func() {
		It("selects Public and Private CUDNs with TargetVRF auto", func() {
			b := testBackend()
			and := testAND("prod", *testSubnet("web", "10.0.1.0/24", andv1beta1.SubnetTypePublic))
			ra := b.buildRouteAdvertisements(and)

			Expect(ra.Name).To(Equal("prod"))
			Expect(ra.Labels).To(HaveKeyWithValue(labelManagedBy, "plexus"))
			Expect(ra.Labels).To(HaveKeyWithValue(labelNetworkDomain, "prod"))
			Expect(ra.Spec.TargetVRF).To(Equal("auto"))
			Expect(ra.Spec.Advertisements).To(Equal([]rav1.AdvertisementType{rav1.PodNetwork}))
			Expect(ra.Spec.FRRConfigurationSelector.MatchLabels).To(HaveKeyWithValue("app", "frr"))
			Expect(ra.Spec.NetworkSelectors).To(HaveLen(1))
			Expect(ra.Spec.NetworkSelectors[0].NetworkSelectionType).To(Equal(crdtypes.ClusterUserDefinedNetworks))
			sel := ra.Spec.NetworkSelectors[0].ClusterUserDefinedNetworkSelector
			Expect(sel.NetworkSelector.MatchLabels).To(HaveKeyWithValue(labelNetworkDomain, "prod"))
			Expect(sel.NetworkSelector.MatchExpressions).To(ContainElement(metav1.LabelSelectorRequirement{
				Key:      labelSubnetType,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{string(andv1beta1.SubnetTypePublic), string(andv1beta1.SubnetTypePrivate)},
			}))
		})
	})
})
