package cli

import (
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

func TestCLI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CLI Suite")
}

var _ = Describe("validate.go", func() {
	Describe("validateSubnetType", func() {
		It("accepts Public, Private, Isolated, and VPNOnly", func() {
			for _, t := range []string{"Public", "Private", "Isolated", "VPNOnly"} {
				st, err := validateSubnetType(t)
				Expect(err).NotTo(HaveOccurred(), t)
				Expect(string(st)).To(Equal(t))
			}
		})

		It("rejects an unknown type", func() {
			_, err := validateSubnetType("Foo")
			Expect(err).To(MatchError(ContainSubstring("invalid subnet type")))
		})
	})

	Describe("validateCIDRs", func() {
		It("requires at least one CIDR", func() {
			Expect(validateCIDRs(nil)).To(MatchError(ContainSubstring("at least one CIDR")))
		})

		It("rejects more than two CIDRs", func() {
			Expect(validateCIDRs([]string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24"})).
				To(MatchError(ContainSubstring("at most two CIDRs")))
		})

		It("rejects an invalid CIDR", func() {
			Expect(validateCIDRs([]string{"not-a-cidr"})).To(MatchError(ContainSubstring("invalid CIDR")))
		})

		It("rejects two IPv4 CIDRs", func() {
			Expect(validateCIDRs([]string{"10.0.0.0/24", "10.0.1.0/24"})).
				To(MatchError(ContainSubstring("two IPv4")))
		})

		It("accepts dual-stack IPv4+IPv6", func() {
			Expect(validateCIDRs([]string{"10.0.0.0/24", "fd00::/64"})).To(Succeed())
		})
	})

	Describe("toCIDRs / cidrStrings", func() {
		It("round-trips CIDR strings", func() {
			in := []string{"10.0.1.0/24", "fd00::/64"}
			cidrs := toCIDRs(in)
			Expect(cidrs).To(Equal([]v1beta1.CIDR{"10.0.1.0/24", "fd00::/64"}))
			Expect(cidrStrings(cidrs)).To(Equal("10.0.1.0/24,fd00::/64"))
		})
	})

	Describe("parseKeyValuePairs", func() {
		It("returns nil for an empty input", func() {
			m, err := parseKeyValuePairs(nil)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeNil())
		})

		It("parses key=value pairs", func() {
			m, err := parseKeyValuePairs([]string{"region=us-east", "zone=a"})
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(Equal(map[string]string{"region": "us-east", "zone": "a"}))
		})

		It("rejects a pair without =", func() {
			_, err := parseKeyValuePairs([]string{"region"})
			Expect(err).To(MatchError(ContainSubstring("must be key=value")))
		})
	})
})

var _ = Describe("describe.go helpers", func() {
	It("formats a nil availability zone as <none>", func() {
		Expect(formatAZ(nil)).To(Equal("<none>"))
	})

	It("formats cluster and node selectors", func() {
		az := &v1beta1.AvailabilityZone{
			ClusterSelector: metav1.LabelSelector{MatchLabels: map[string]string{"region": "us-east"}},
			NodeSelector:    map[string]string{"topology.kubernetes.io/zone": "rack-a"},
		}
		got := formatAZ(az)
		Expect(got).To(ContainSubstring("cluster(region=us-east)"))
		Expect(got).To(ContainSubstring("node(topology.kubernetes.io/zone=rack-a)"))
	})

	It("formats durations", func() {
		Expect(formatDuration(30 * time.Second)).To(Equal("30s"))
		Expect(formatDuration(5 * time.Minute)).To(Equal("5m"))
		Expect(formatDuration(3 * time.Hour)).To(Equal("3h"))
		Expect(formatDuration(48 * time.Hour)).To(Equal("2d"))
	})
})

var _ = Describe("root.go", func() {
	It("registers the documented subcommands", func() {
		cmd := NewRootCommand()
		names := map[string]bool{}
		for _, c := range cmd.Commands() {
			names[c.Name()] = true
		}
		Expect(names).To(HaveKey("create"))
		Expect(names).To(HaveKey("delete"))
		Expect(names).To(HaveKey("describe"))
		Expect(names).To(HaveKey("add-subnet"))
		Expect(names).To(HaveKey("delete-subnet"))
		Expect(names).To(HaveKey("version"))
	})
})
