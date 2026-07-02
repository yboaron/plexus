package ovnkubernetes

import (
	"fmt"

	bitmapallocator "github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/allocator/bitmap"
	"github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/syncmap"

	v1beta1 "github.com/ovn-kubernetes/plexus/api/administrativenetworkdomain/v1beta1"
)

const (
	// VNI range: [vniMin, vniMax]. We reserve 0–4095 to avoid collisions
	// with manually assigned VNIs or VLAN IDs.
	vniMin = 4096
	vniMax = 16777215 // 2^24 - 1
)

// TODO: export NewIDAllocator with offset support from
// github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/allocator/id
// and replace this local copy. This is adapted from the unexported
// idAllocator (single-ID) and idsAllocator (offset support) in that
// package. Multi-ID support and helper interfaces were dropped since
// VNI allocation is always one ID per key.

// idAllocator allocates unique integer IDs from a bitmap, offset by a
// fixed amount. Thread-safe via per-key locking.
type idAllocator struct {
	nameIDMap *syncmap.SyncMap[int]
	idBitmap  *bitmapallocator.AllocationBitmap
	offset    int
}

func newIDAllocator(name string, maxIDs, offset int) *idAllocator {
	return &idAllocator{
		nameIDMap: syncmap.NewSyncMap[int](),
		idBitmap:  bitmapallocator.NewRoundRobinAllocationMap(maxIDs, name),
		offset:    offset,
	}
}

func (a *idAllocator) AllocateID(name string) (int, error) {
	a.nameIDMap.LockKey(name)
	defer a.nameIDMap.UnlockKey(name)

	if v, ok := a.nameIDMap.Load(name); ok {
		return v, nil
	}

	id, allocated, _ := a.idBitmap.AllocateNext()
	if !allocated {
		return 0, fmt.Errorf("failed to allocate ID for %q: pool exhausted", name)
	}

	final := id + a.offset
	a.nameIDMap.Store(name, final)
	return final, nil
}

func (a *idAllocator) ReserveID(name string, id int) error {
	a.nameIDMap.LockKey(name)
	defer a.nameIDMap.UnlockKey(name)

	if v, ok := a.nameIDMap.Load(name); ok {
		if v == id {
			return nil
		}
		return fmt.Errorf("can't reserve ID %d for %q: already allocated with ID %d", id, name, v)
	}

	reserved, _ := a.idBitmap.Allocate(id - a.offset)
	if !reserved {
		return fmt.Errorf("ID %d is already reserved by another resource", id)
	}

	a.nameIDMap.Store(name, id)
	return nil
}

func (a *idAllocator) ReleaseID(name string) {
	a.nameIDMap.LockKey(name)
	defer a.nameIDMap.UnlockKey(name)

	if v, ok := a.nameIDMap.Load(name); ok {
		a.idBitmap.Release(v - a.offset)
		a.nameIDMap.Delete(name)
	}
}

// VNIAllocator manages VNI allocation for EVPN subnets. A single
// bitmap pool guarantees global VNI uniqueness across both MACVRF and
// IPVRF assignments. IDs are allocated directly in range [vniMin, vniMax].
//
// The allocator state is in-memory. On controller restart, the backend
// must rebuild state by reading existing UDN resources and calling
// ReserveID for each VNI already in use. This is the same pattern
// used by ovnkube-cluster-manager for its ID allocators.
type VNIAllocator struct {
	allocator *idAllocator
}

// NewVNIAllocator creates a VNI allocator backed by a single bitmap pool.
func NewVNIAllocator() *VNIAllocator {
	return &VNIAllocator{
		allocator: newIDAllocator("plexus-vni", vniMax-vniMin+1, vniMin),
	}
}

// SubnetVNIs holds the allocated VNI(s) for a single subnet.
type SubnetVNIs struct {
	MACVRF int
	IPVRF  int // zero for Isolated subnets only
}

func subnetKey(andName, subnetName, vrfType string) string {
	return andName + "/" + subnetName + "/" + vrfType
}

// AllocateSubnetVNIs allocates VNIs for a subnet. Public and Private
// subnets get both MACVRF and IPVRF VNIs (Private needs ipVRF for
// intra-domain route leaking); Isolated subnets get only a MACVRF VNI.
// Allocations are idempotent — calling with the same key returns the
// previously allocated VNIs.
//
// TODO: there is a narrow race where the controller crashes after
// allocating a VNI here but before creating the CUDN. On restart the
// allocator rebuilds from existing CUDNs, so the orphaned VNI is never
// reclaimed. This is practically harmless given the 24-bit VNI space,
// but could be fixed by persisting pending allocations to a ConfigMap
// before creating the CUDN.
func (a *VNIAllocator) AllocateSubnetVNIs(andName, subnetName string, subnetType v1beta1.SubnetType) (SubnetVNIs, error) {
	macVNI, err := a.allocator.AllocateID(subnetKey(andName, subnetName, "macvrf"))
	if err != nil {
		return SubnetVNIs{}, err
	}

	vnis := SubnetVNIs{
		MACVRF: macVNI,
	}

	if subnetType != v1beta1.SubnetTypeIsolated {
		ipVNI, err := a.allocator.AllocateID(subnetKey(andName, subnetName, "ipvrf"))
		if err != nil {
			return SubnetVNIs{}, err
		}
		vnis.IPVRF = ipVNI
	}

	return vnis, nil
}

// ReleaseSubnetVNIs frees VNIs allocated for a subnet.
func (a *VNIAllocator) ReleaseSubnetVNIs(andName, subnetName string) {
	a.allocator.ReleaseID(subnetKey(andName, subnetName, "macvrf"))
	a.allocator.ReleaseID(subnetKey(andName, subnetName, "ipvrf"))
}
