package multicluster

import (
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Cluster Secrets below live in "default", which kube-apiserver bootstraps, so
// the suite needs no namespace setup. The inventory leaves Namespace unset and
// therefore lists across all namespaces, so the namespace is arbitrary here;
// isolation comes from the label sweep in BeforeEach/AfterEach. A dedicated
// namespace would not isolate anything and envtest, having no namespace
// controller, could never finalize its deletion.
var _ = Describe("SecretInventory", func() {
	var inv *SecretInventory

	BeforeEach(func() {
		if k8sClient == nil {
			Skip("KUBEBUILDER_ASSETS not set — run 'make test' to include envtest")
		}

		var secrets corev1.SecretList
		Expect(k8sClient.List(ctx, &secrets, client.MatchingLabels{LabelCluster: "true"})).To(Succeed())
		for i := range secrets.Items {
			Expect(k8sClient.Delete(ctx, &secrets.Items[i])).To(Succeed())
		}

		inv = NewSecretInventory(SecretInventoryOptions{
			HubClient: k8sClient,
			HubLabels: map[string]string{"region": "hub"},
			Scheme:    scheme,
			Log:       logr.Discard(),
		})
		Expect(inv.Sync(ctx)).To(Succeed())
	})

	AfterEach(func() {
		if k8sClient == nil {
			return
		}
		var secrets corev1.SecretList
		if err := k8sClient.List(ctx, &secrets, client.MatchingLabels{LabelCluster: "true"}); err == nil {
			for i := range secrets.Items {
				_ = k8sClient.Delete(ctx, &secrets.Items[i])
			}
		}
	})

	It("always includes the hub cluster", func() {
		all, err := inv.AllClusters()
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].Name).To(Equal(hubClusterName))
		Expect(all[0].IsHub).To(BeTrue())
		Expect(all[0].Labels).To(HaveKeyWithValue("region", "hub"))
		Expect(all[0].Client).To(Equal(k8sClient))
	})

	It("returns the hub from GetCluster", func() {
		ci, err := inv.GetCluster(hubClusterName)
		Expect(err).NotTo(HaveOccurred())
		Expect(ci.IsHub).To(BeTrue())
	})

	It("returns an error for an unknown cluster", func() {
		_, err := inv.GetCluster("missing")
		Expect(err).To(MatchError(ContainSubstring("not found")))
	})

	It("matches all clusters when the selector is nil", func() {
		matched, err := inv.MatchClusters(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(HaveLen(1))
		Expect(matched[0].Name).To(Equal(hubClusterName))
	})

	It("filters clusters by label selector", func() {
		matched, err := inv.MatchClusters(&metav1.LabelSelector{
			MatchLabels: map[string]string{"region": "hub"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(HaveLen(1))

		matched, err = inv.MatchClusters(&metav1.LabelSelector{
			MatchLabels: map[string]string{"region": "spoke"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeEmpty())
	})

	It("skips Secrets named hub and Secrets without a usable kubeconfig", func() {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      hubClusterName,
				Namespace: "default",
				Labels:    map[string]string{LabelCluster: "true", "region": "ignored"},
			},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spoke-1",
				Namespace: "default",
				Labels:    map[string]string{LabelCluster: "true", "region": "us-west"},
			},
			Data: map[string][]byte{"not-kubeconfig": []byte("nope")},
		})).To(Succeed())

		Expect(inv.Sync(ctx)).To(Succeed())
		all, err := inv.AllClusters()
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].Name).To(Equal(hubClusterName))
	})

	It("discovers a spoke cluster from a kubeconfig Secret", func() {
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spoke-east",
				Namespace: "default",
				Labels:    map[string]string{LabelCluster: "true", "region": "us-east"},
			},
			Data: map[string][]byte{SecretDataKey: kubeconfigFromRest(testCfg)},
		})).To(Succeed())

		Expect(inv.Sync(ctx)).To(Succeed())

		spoke, err := inv.GetCluster("spoke-east")
		Expect(err).NotTo(HaveOccurred())
		Expect(spoke.IsHub).To(BeFalse())
		Expect(spoke.Labels).To(HaveKeyWithValue("region", "us-east"))
		Expect(spoke.Client).NotTo(BeNil())

		var ns corev1.Namespace
		Expect(spoke.Client.Get(ctx, client.ObjectKey{Name: "default"}, &ns)).To(Succeed())

		matched, err := inv.MatchClusters(&metav1.LabelSelector{
			MatchLabels: map[string]string{"region": "us-east"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(HaveLen(1))
		Expect(matched[0].Name).To(Equal("spoke-east"))
	})

	It("drops a spoke cluster once its Secret is deleted", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "spoke-west",
				Namespace: "default",
				Labels:    map[string]string{LabelCluster: "true", "region": "us-west"},
			},
			Data: map[string][]byte{SecretDataKey: kubeconfigFromRest(testCfg)},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())

		Expect(inv.Sync(ctx)).To(Succeed())
		_, err := inv.GetCluster("spoke-west")
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
		Expect(inv.Sync(ctx)).To(Succeed())

		_, err = inv.GetCluster("spoke-west")
		Expect(err).To(MatchError(ContainSubstring("not found")))

		// Only the hub is left, and it never matches the spoke's labels.
		all, err := inv.AllClusters()
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveLen(1))
		Expect(all[0].Name).To(Equal(hubClusterName))

		matched, err := inv.MatchClusters(&metav1.LabelSelector{
			MatchLabels: map[string]string{"region": "us-west"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(matched).To(BeEmpty())
	})
})

var _ = Describe("clusterLabelsFromSecret", func() {
	It("copies Secret labels except the inventory marker", func() {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					LabelCluster: "true",
					"region":     "us-east",
					"zone":       "a",
				},
			},
		}
		Expect(clusterLabelsFromSecret(secret)).To(Equal(map[string]string{
			"region": "us-east",
			"zone":   "a",
		}))
	})
})
