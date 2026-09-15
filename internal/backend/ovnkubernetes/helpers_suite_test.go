package ovnkubernetes

import (
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
	configv1beta1 "github.com/ovn-kubernetes/plexus/api/plexuscontrollerconfig/v1beta1"
)

func testConfig() *configv1beta1.OVNKubernetesConfig {
	return &configv1beta1.OVNKubernetesConfig{
		VTEPCIDRs: []configv1beta1.CIDR{"172.18.0.0/16"},
		FRRConfigurationSelector: metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "frr"},
		},
	}
}

func testBackend() *OVNKubernetesBackend {
	return New(nil, logr.Discard(), testConfig(), nil)
}

func testAND(name string, subnets ...andv1beta1.Subnet) *andv1beta1.AdministrativeNetworkDomain {
	return &andv1beta1.AdministrativeNetworkDomain{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       andv1beta1.AdministrativeNetworkDomainSpec{Subnets: subnets},
	}
}

func testSubnet(name, cidr string, subnetType andv1beta1.SubnetType) *andv1beta1.Subnet {
	return &andv1beta1.Subnet{
		Name:  name,
		CIDRs: []andv1beta1.CIDR{andv1beta1.CIDR(cidr)},
		Type:  subnetType,
	}
}
