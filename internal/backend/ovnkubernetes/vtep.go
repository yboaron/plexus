package ovnkubernetes

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vtepv1 "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1"

	andv1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

const labelManagedBy = "plexus.io/managed-by"

func (b *OVNKubernetesBackend) reconcileVTEP(ctx context.Context, cl client.Client) error {
	cidrs := make([]vtepv1.CIDR, len(b.config.VTEPCIDRs))
	for i, c := range b.config.VTEPCIDRs {
		cidrs[i] = vtepv1.CIDR(c)
	}

	existing := &vtepv1.VTEP{}
	err := cl.Get(ctx, client.ObjectKey{Name: vtepName}, existing)
	if apierrors.IsNotFound(err) {
		vtep := &vtepv1.VTEP{
			ObjectMeta: metav1.ObjectMeta{
				Name: vtepName,
				Labels: map[string]string{
					labelManagedBy: "plexus",
				},
			},
			Spec: vtepv1.VTEPSpec{
				CIDRs: cidrs,
				Mode:  vtepv1.VTEPModeUnmanaged,
			},
		}
		b.log.Info("creating shared VTEP", "name", vtepName)
		if err := cl.Create(ctx, vtep); err != nil {
			return fmt.Errorf("creating VTEP %q: %w", vtepName, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("getting VTEP %q: %w", vtepName, err)
	}

	if !cidrsEqual(existing.Spec.CIDRs, cidrs) {
		existing.Spec.CIDRs = cidrs
		b.log.Info("updating VTEP CIDRs", "name", vtepName)
		if err := cl.Update(ctx, existing); err != nil {
			return fmt.Errorf("updating VTEP %q: %w", vtepName, err)
		}
	}

	return nil
}

// deleteVTEP removes the shared VTEP if no other ANDs reference it.
// The ref-count check uses the hub client since AND CRs only exist on the hub.
func (b *OVNKubernetesBackend) deleteVTEP(ctx context.Context, deletingAND string, cl client.Client) error {
	var andList andv1beta1.AdministrativeNetworkDomainList
	if err := b.client.List(ctx, &andList); err != nil {
		return fmt.Errorf("listing ANDs for VTEP ref count: %w", err)
	}

	remaining := 0
	for i := range andList.Items {
		if andList.Items[i].Name != deletingAND && andList.Items[i].DeletionTimestamp.IsZero() {
			remaining++
		}
	}
	if remaining > 0 {
		b.log.Info("skipping VTEP deletion, other ANDs still exist", "remaining", remaining)
		return nil
	}

	vtep := &vtepv1.VTEP{
		ObjectMeta: metav1.ObjectMeta{
			Name: vtepName,
		},
	}
	err := cl.Delete(ctx, vtep)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("deleting VTEP %q: %w", vtepName, err)
	}
	return nil
}

// cidrsEqual reports whether two CIDR lists carry the same CIDRs.
// VTEP.Spec.CIDRs is a set in all but name — the order carries no meaning —
// so reordering VTEPCIDRs in the Plexus config must not count as drift and
// trigger an Update on every reconcile. Duplicates are compared by
// multiplicity so that a list is never considered equal to one holding a
// different number of copies of the same CIDR.
func cidrsEqual(a []vtepv1.CIDR, b []vtepv1.CIDR) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[vtepv1.CIDR]int, len(a))
	for _, c := range a {
		counts[c]++
	}
	for _, c := range b {
		counts[c]--
		if counts[c] < 0 {
			return false
		}
	}
	return true
}
