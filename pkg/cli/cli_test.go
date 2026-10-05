package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

// fakeClientFn returns a clientFn that always hands back a fake client
// pre-populated with the given objects. No envtest, no kube-apiserver —
// instantaneous and useful for exercising the command RunE paths.
//
// Note: do NOT call WithStatusSubresource() here. Calling it (even with no
// args) enables the status-as-subresource gate, which strips Status from
// objects that are not explicitly listed. Since the CLI only reads status
// (via Get, never Status().Update()), we want the full object back.
func fakeClientFn(objs ...client.Object) func() (client.Client, error) {
	c := fake.NewClientBuilder().
		WithScheme(cliScheme).
		WithObjects(objs...).
		Build()
	return func() (client.Client, error) { return c, nil }
}

func errClientFn(msg string) func() (client.Client, error) {
	return func() (client.Client, error) {
		return nil, fmt.Errorf("%s", msg)
	}
}

// testAND returns a minimal AND with a fixed creation timestamp so
// describe tests don't have to parse relative time strings.
func testANDObj(name string, subnets ...v1beta1.Subnet) *v1beta1.AdministrativeNetworkDomain {
	return &v1beta1.AdministrativeNetworkDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			ResourceVersion:   "1",
			CreationTimestamp: metav1.Now(),
		},
		Spec: v1beta1.AdministrativeNetworkDomainSpec{Subnets: subnets},
	}
}

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

	It("formats an empty availability zone (no selectors) as <empty>", func() {
		Expect(formatAZ(&v1beta1.AvailabilityZone{})).To(Equal("<empty>"))
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

var _ = Describe("version command", func() {
	It("prints a version line", func() {
		var buf bytes.Buffer
		cmd := newVersionCommand()
		cmd.SetOut(&buf)
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("plexus version"))
	})
})

var _ = Describe("create command", func() {
	It("creates an AND and prints confirmation", func() {
		var buf bytes.Buffer
		cmd := newCreateCommand(fakeClientFn())
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"my-network"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("administrativenetworkdomain/my-network created"))
	})

	It("returns an error when the client fails", func() {
		cmd := newCreateCommand(errClientFn("no kubeconfig"))
		cmd.SetArgs([]string{"my-network"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("no kubeconfig")))
	})
})

var _ = Describe("delete command", func() {
	It("deletes an AND with --yes and prints confirmation", func() {
		and := testANDObj("prod")
		var buf bytes.Buffer
		cmd := newDeleteCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--yes", "prod"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("administrativenetworkdomain/prod deleted"))
	})

	It("prompts for confirmation and deletes when the user answers y", func() {
		and := testANDObj("prod")
		var buf bytes.Buffer
		cmd := newDeleteCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("Continue? [y/N]"))
		Expect(buf.String()).To(ContainSubstring("prod deleted"))
	})

	It("aborts when the user answers n", func() {
		and := testANDObj("prod")
		var buf bytes.Buffer
		cmd := newDeleteCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetIn(strings.NewReader("n\n"))
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("Aborted."))
		Expect(buf.String()).NotTo(ContainSubstring("deleted"))
	})

	It("returns an error when the client fails", func() {
		cmd := newDeleteCommand(errClientFn("no kubeconfig"))
		cmd.SetArgs([]string{"--yes", "prod"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("no kubeconfig")))
	})
})

var _ = Describe("describe command", func() {
	It("prints AND details with no subnets", func() {
		and := testANDObj("prod")
		var buf bytes.Buffer
		cmd := newDescribeCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(Succeed())
		out := buf.String()
		Expect(out).To(ContainSubstring("Name:"))
		Expect(out).To(ContainSubstring("prod"))
		Expect(out).To(ContainSubstring("Subnets:      0"))
	})

	It("prints the subnet table when subnets are present", func() {
		and := testANDObj("prod",
			v1beta1.Subnet{Name: "web", CIDRs: []v1beta1.CIDR{"10.0.1.0/24"}, Type: v1beta1.SubnetTypePublic},
		)
		var buf bytes.Buffer
		cmd := newDescribeCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(Succeed())
		out := buf.String()
		Expect(out).To(ContainSubstring("SUBNET"))
		Expect(out).To(ContainSubstring("web"))
		Expect(out).To(ContainSubstring("10.0.1.0/24"))
		Expect(out).To(ContainSubstring("Public"))
	})

	It("prints the status conditions table when conditions are present", func() {
		and := testANDObj("prod")
		and.Status.Conditions = []metav1.Condition{
			{
				Type:               "Ready",
				Status:             metav1.ConditionTrue,
				Reason:             "Reconciled",
				Message:            "all subnets reconciled",
				LastTransitionTime: metav1.Now(),
			},
		}
		var buf bytes.Buffer
		// Pass the object directly: the fake client stores it as-is including Status.
		cmd := newDescribeCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(Succeed())
		out := buf.String()
		Expect(out).To(ContainSubstring("Conditions:"))
		Expect(out).To(ContainSubstring("Ready"))
		Expect(out).To(ContainSubstring("Reconciled"))
	})

	It("returns an error for a non-existent AND", func() {
		cmd := newDescribeCommand(fakeClientFn())
		cmd.SetArgs([]string{"missing"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("missing")))
	})

	It("returns an error when the client fails", func() {
		cmd := newDescribeCommand(errClientFn("no kubeconfig"))
		cmd.SetArgs([]string{"prod"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("no kubeconfig")))
	})
})

var _ = Describe("add-subnet command", func() {
	It("adds a subnet and prints confirmation", func() {
		and := testANDObj("prod")
		var buf bytes.Buffer
		cmd := newAddSubnetCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"prod", "web", "--cidr", "10.0.1.0/24", "--type", "Public"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("subnet/web added to administrativenetworkdomain/prod"))
	})

	It("rejects an invalid CIDR before touching the client", func() {
		cmd := newAddSubnetCommand(errClientFn("should not be called"))
		cmd.SetArgs([]string{"prod", "web", "--cidr", "not-a-cidr"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("invalid CIDR")))
	})

	It("rejects an invalid subnet type before touching the client", func() {
		cmd := newAddSubnetCommand(errClientFn("should not be called"))
		cmd.SetArgs([]string{"prod", "web", "--cidr", "10.0.1.0/24", "--type", "Bad"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("invalid subnet type")))
	})

	It("rejects --node-selector without --cluster-selector", func() {
		and := testANDObj("prod")
		cmd := newAddSubnetCommand(fakeClientFn(and))
		cmd.SetArgs([]string{"prod", "web", "--cidr", "10.0.1.0/24",
			"--node-selector", "zone=a"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("--cluster-selector is required")))
	})

	It("returns an error when the AND does not exist", func() {
		cmd := newAddSubnetCommand(fakeClientFn())
		cmd.SetArgs([]string{"missing", "web", "--cidr", "10.0.1.0/24"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("missing")))
	})

	It("returns an error when the subnet already exists", func() {
		and := testANDObj("prod",
			v1beta1.Subnet{Name: "web", CIDRs: []v1beta1.CIDR{"10.0.1.0/24"}, Type: v1beta1.SubnetTypePublic},
		)
		cmd := newAddSubnetCommand(fakeClientFn(and))
		cmd.SetArgs([]string{"prod", "web", "--cidr", "10.0.2.0/24"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("already exists")))
	})

	It("returns an error when the client fails", func() {
		cmd := newAddSubnetCommand(errClientFn("no kubeconfig"))
		cmd.SetArgs([]string{"prod", "web", "--cidr", "10.0.1.0/24"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("no kubeconfig")))
	})
})

var _ = Describe("delete-subnet command", func() {
	It("removes a subnet with --yes and prints confirmation", func() {
		and := testANDObj("prod",
			v1beta1.Subnet{Name: "web", CIDRs: []v1beta1.CIDR{"10.0.1.0/24"}, Type: v1beta1.SubnetTypePublic},
		)
		var buf bytes.Buffer
		cmd := newDeleteSubnetCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{"--yes", "prod", "web"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("subnet/web deleted from administrativenetworkdomain/prod"))
	})

	It("prompts for confirmation and deletes when the user answers y", func() {
		and := testANDObj("prod",
			v1beta1.Subnet{Name: "web", CIDRs: []v1beta1.CIDR{"10.0.1.0/24"}, Type: v1beta1.SubnetTypePublic},
		)
		var buf bytes.Buffer
		cmd := newDeleteSubnetCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetArgs([]string{"prod", "web"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("Continue? [y/N]"))
		Expect(buf.String()).To(ContainSubstring("web deleted from"))
	})

	It("aborts when the user answers n", func() {
		and := testANDObj("prod",
			v1beta1.Subnet{Name: "web", CIDRs: []v1beta1.CIDR{"10.0.1.0/24"}, Type: v1beta1.SubnetTypePublic},
		)
		var buf bytes.Buffer
		cmd := newDeleteSubnetCommand(fakeClientFn(and))
		cmd.SetOut(&buf)
		cmd.SetIn(strings.NewReader("n\n"))
		cmd.SetArgs([]string{"prod", "web"})
		Expect(cmd.Execute()).To(Succeed())
		Expect(buf.String()).To(ContainSubstring("Aborted."))
	})

	It("returns an error when the subnet is not found", func() {
		and := testANDObj("prod")
		cmd := newDeleteSubnetCommand(fakeClientFn(and))
		cmd.SetArgs([]string{"--yes", "prod", "nonexistent"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("not found")))
	})

	It("returns an error when the AND is not found", func() {
		cmd := newDeleteSubnetCommand(fakeClientFn())
		cmd.SetArgs([]string{"--yes", "missing", "web"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("missing")))
	})

	It("returns an error when the client fails", func() {
		cmd := newDeleteSubnetCommand(errClientFn("no kubeconfig"))
		cmd.SetArgs([]string{"--yes", "prod", "web"})
		Expect(cmd.Execute()).To(MatchError(ContainSubstring("no kubeconfig")))
	})
})
