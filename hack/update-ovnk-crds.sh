#!/usr/bin/env bash
# Generate OVN-Kubernetes CRD YAML files used by envtest.
#
# Method: allowlist of Go packages whose types Plexus actually talks to.
# Do not generate every OVN-Kubernetes CRD (EgressIP, NetworkQoS, …) — only
# packages that appear in the controller scheme or that the backend creates.
#
# When Plexus starts using a new OVN-Kubernetes CRD:
#   1. Add that type's Go package to the controller-gen paths list below
#   2. Re-run this script
#   3. Commit the new/updated YAML under test/testdata/crds/
#
# CRDs are generated from the Go types in the module cache using
# controller-gen, so the schemas match the ovn-kubernetes version pinned
# in go.mod. Offline once the module cache is warm.
#
# Run this script whenever the ovn-kubernetes version in go.mod changes:
#
#   go get github.com/ovn-kubernetes/ovn-kubernetes/go-controller@<new-version>
#   hack/update-ovnk-crds.sh
#   git add go.mod go.sum test/testdata/crds/
#   git commit -s -m "Bump ovn-kubernetes to <new-version>"

set -euo pipefail

DEST="$(git rev-parse --show-toplevel)/test/testdata/crds"

echo "Generating OVN-Kubernetes CRDs from module cache into ${DEST}/"
mkdir -p "${DEST}"

# Allowlist of OVN-Kubernetes CRD packages to install in envtest.
# When Plexus uses a new OVN-K CRD, add its Go package path to this list.
#
# userdefinednetwork/v1  → ClusterUserDefinedNetwork (CUDN) and UserDefinedNetwork (UDN)
# routeadvertisements/v1 → RouteAdvertisements
# vtep/v1                → VTEP
go tool controller-gen crd \
  paths="github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/userdefinednetwork/v1" \
  paths="github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/routeadvertisements/v1" \
  paths="github.com/ovn-kubernetes/ovn-kubernetes/go-controller/pkg/crd/vtep/v1" \
  output:crd:dir="${DEST}"

echo "Done. Files written to ${DEST}/:"
ls "${DEST}/"
echo "Commit these files alongside any go.mod changes to keep CRD schemas in sync."
