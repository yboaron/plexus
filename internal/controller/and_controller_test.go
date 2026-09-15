package controller_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
	"github.com/ovn-kubernetes/plexus/internal/backend"
)

const (
	timeout   = 10 * time.Second
	interval  = 250 * time.Millisecond
	finalizer = "plexus.io/and-protection"
)

var _ = Describe("ANDReconciler", func() {
	// andObj holds the AND created in the current test so AfterEach can clean it up.
	var andObj *andv1beta1.AdministrativeNetworkDomain

	BeforeEach(func() {
		fakeBack.reset()
		andObj = nil
	})

	AfterEach(func() {
		// Reset the backend first so backend.Delete() succeeds during cleanup,
		// even if the test left it in an error state.
		fakeBack.reset()
		if andObj != nil {
			_ = k8sClient.Delete(ctx, andObj)
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), &andv1beta1.AdministrativeNetworkDomain{})
			}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
			andObj = nil
		}
	})

	Describe("finalizer lifecycle", func() {
		It("adds the protection finalizer on creation", func() {
			andObj = makeAND("and-finalizer-test")
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, timeout, interval).Should(Succeed())
		})

		It("calls backend.Delete and removes the finalizer when the AND is deleted", func() {
			andObj = makeAND("and-delete-test")
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			// Wait until the finalizer is set before triggering deletion.
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, timeout, interval).Should(Succeed())

			Expect(k8sClient.Delete(ctx, andObj)).To(Succeed())

			// Verify backend.Delete was called by the reconciler.
			Eventually(fakeBack.deleteCalled, timeout, interval).Should(Receive())

			// The AND must be fully removed once the finalizer is released.
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), &andv1beta1.AdministrativeNetworkDomain{})
			}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
			andObj = nil // already gone; skip AfterEach deletion
		})
	})

	Describe("status conditions", func() {
		It("sets Ready=False/NoSubnets when the AND has no subnets", func() {
			andObj = makeAND("and-nosubnets-test")
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				cond := apimeta.FindStatusCondition(andObj.Status.Conditions, "Ready")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("NoSubnets"))
			}, timeout, interval).Should(Succeed())
		})

		It("sets Ready=True/Reconciled when backend succeeds and subnets are defined", func() {
			andObj = makeAND("and-ready-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				cond := apimeta.FindStatusCondition(andObj.Status.Conditions, "Ready")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(cond.Reason).To(Equal("Reconciled"))
			}, timeout, interval).Should(Succeed())
		})

		It("sets Ready=False/ReconcileError when the backend returns an error", func() {
			fakeBack.setReconcile(backend.Result{}, fmt.Errorf("backend exploded"))
			andObj = makeAND("and-error-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				cond := apimeta.FindStatusCondition(andObj.Status.Conditions, "Ready")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("ReconcileError"))
			}, timeout, interval).Should(Succeed())
		})

		It("sets Ready=False/<StatusReason> when the backend reports a not-ready state", func() {
			fakeBack.setReconcile(backend.Result{
				StatusReason:  "VTEPNotReady",
				StatusMessage: "waiting for VTEP to become ready",
			}, nil)
			andObj = makeAND("and-notready-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				cond := apimeta.FindStatusCondition(andObj.Status.Conditions, "Ready")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Status).To(Equal(metav1.ConditionFalse))
				g.Expect(cond.Reason).To(Equal("VTEPNotReady"))
			}, timeout, interval).Should(Succeed())
		})
	})

	Describe("backend delete failure", func() {
		It("retains the finalizer when backend.Delete fails", func() {
			andObj = makeAND("and-delete-error-test")
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())

			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, timeout, interval).Should(Succeed())

			fakeBack.setDeleteErr(fmt.Errorf("backend delete failed"))
			Expect(k8sClient.Delete(ctx, andObj)).To(Succeed())

			Consistently(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, 2*time.Second, interval).Should(Succeed())

			fakeBack.setDeleteErr(nil)
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), &andv1beta1.AdministrativeNetworkDomain{})
			}, timeout, interval).Should(MatchError(ContainSubstring("not found")))
			andObj = nil
		})
	})

	Describe("child resource watches", func() {
		It("re-reconciles when a labeled Namespace is created", func() {
			andObj = makeAND("and-watch-ns-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				cond := apimeta.FindStatusCondition(andObj.Status.Conditions, "Ready")
				g.Expect(cond).NotTo(BeNil())
				g.Expect(cond.Reason).To(Equal("Reconciled"))
			}, timeout, interval).Should(Succeed())

			before := fakeBack.getReconcileCount()
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "and-watch-ns-test-sn",
					Labels: map[string]string{
						"plexus.io/network-domain": "and-watch-ns-test",
					},
				},
			}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, ns)
			})

			Eventually(fakeBack.getReconcileCount, timeout, interval).Should(BeNumerically(">", before))
		})

		It("re-reconciles when a plexus-managed VTEP changes", func() {
			andObj = makeAND("and-watch-vtep-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, timeout, interval).Should(Succeed())

			before := fakeBack.getReconcileCount()
			vtep := &vtepv1.VTEP{
				ObjectMeta: metav1.ObjectMeta{
					Name: "watch-vtep",
					Labels: map[string]string{
						"plexus.io/managed-by": "plexus",
					},
				},
				Spec: vtepv1.VTEPSpec{
					CIDRs: []vtepv1.CIDR{"172.18.0.0/16"},
					Mode:  vtepv1.VTEPModeUnmanaged,
				},
			}
			Expect(k8sClient.Create(ctx, vtep)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, vtep)
			})

			Eventually(fakeBack.getReconcileCount, timeout, interval).Should(BeNumerically(">", before))
		})

		It("re-reconciles when a cluster inventory Secret is created", func() {
			andObj = makeAND("and-watch-secret-test", makeSubnet("sn"))
			Expect(k8sClient.Create(ctx, andObj)).To(Succeed())
			Eventually(func(g Gomega) {
				g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(andObj), andObj)).To(Succeed())
				g.Expect(andObj.Finalizers).To(ContainElement(finalizer))
			}, timeout, interval).Should(Succeed())

			before := fakeBack.getReconcileCount()
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "spoke-cluster",
					Namespace: "default",
					Labels: map[string]string{
						"plexus.io/cluster": "true",
					},
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())
			DeferCleanup(func() {
				_ = k8sClient.Delete(ctx, secret)
			})

			Eventually(fakeBack.getReconcileCount, timeout, interval).Should(BeNumerically(">", before))
		})
	})
})

// makeAND returns a minimal AND object with the given name and optional subnets.
func makeAND(name string, subnets ...andv1beta1.Subnet) *andv1beta1.AdministrativeNetworkDomain {
	return &andv1beta1.AdministrativeNetworkDomain{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: andv1beta1.AdministrativeNetworkDomainSpec{
			Subnets: subnets,
		},
	}
}

// makeSubnet returns a minimal valid subnet fixture.
func makeSubnet(name string) andv1beta1.Subnet {
	return andv1beta1.Subnet{
		Name:  name,
		CIDRs: []andv1beta1.CIDR{"10.0.0.0/24"},
		Type:  andv1beta1.SubnetTypePublic,
	}
}
