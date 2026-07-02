#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Create a multi-cluster (hub + N spokes) KIND environment for Plexus.
# All clusters share a single external FRR route reflector on the Docker
# "kind" network, with non-overlapping pod/service CIDRs.
#
# Usage:
#   contrib/plexus-kind-multi.sh [OPTIONS]
#
# Options:
#   --hub NAME           Hub cluster name (default: plexus-hub)
#   --spokes N           Number of spoke clusters (default: 1)
#   --spoke-prefix P     Spoke cluster name prefix (default: plexus-spoke)
#   --workers N          Worker nodes per cluster (default: 1)
#   --skip-build         Reuse existing OVN image
#   --force-build        Rebuild OVN image even if it exists
#   --delete             Tear down all clusters and exit

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/plexus-kind-common.sh"

HUB_NAME="plexus-hub"
SPOKE_COUNT=1
SPOKE_PREFIX="plexus-spoke"
WORKERS=1
SKIP_BUILD=false
DELETE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --hub)          HUB_NAME="$2"; shift 2 ;;
    --spokes)       SPOKE_COUNT="$2"; shift 2 ;;
    --spoke-prefix) SPOKE_PREFIX="$2"; shift 2 ;;
    --workers)      WORKERS="$2"; shift 2 ;;
    --skip-build)   SKIP_BUILD=true; shift ;;
    --force-build)  FORCE_BUILD=true; shift ;;
    --delete)       DELETE=true; shift ;;
    *)              echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

HUB_KUBECONFIG="${HOME}/${HUB_NAME}.conf"

spoke_name() { echo "${SPOKE_PREFIX}-${1}"; }
spoke_kubeconfig() { echo "${HOME}/$(spoke_name "$1").conf"; }
cluster_network() { echo "plexus-${1}"; }

all_cluster_names() {
  echo "$HUB_NAME"
  for ((i = 1; i <= SPOKE_COUNT; i++)); do
    spoke_name "$i"
  done
}

if [ "$DELETE" = true ]; then
  echo "=== Deleting all Plexus clusters ==="
  for name in $(all_cluster_names); do
    kind delete cluster --name "$name" 2>/dev/null || true
    rm -f "${HOME}/${name}.conf"
    delete_docker_network "$(cluster_network "$name")"
  done
  cleanup_external_frr
  echo "Done."
  exit 0
fi

resolve_ovn_kubernetes_path
echo "Using OVN-Kubernetes from: ${OVN_KUBERNETES_PATH}"
echo "Hub: ${HUB_NAME}, Spokes: ${SPOKE_COUNT}, Workers/cluster: ${WORKERS}"
echo ""

echo "=== Phase 1: Images ==="
if [ "$SKIP_BUILD" = true ]; then
  OVN_IMAGE="localhost/ovn-daemonset-fedora:dev"
  PLEXUS_IMAGE="localhost/plexus-controller:dev"
  echo "Skipping image builds, using existing images"
else
  build_ovn_image
  build_plexus_image
fi
echo ""

echo "=== Phase 2: Creating KIND clusters ==="

create_cluster() {
  local name=$1 index=$2 kubeconfig=$3 network=$4

  compute_cidrs "$index"
  create_docker_network "$network" "$DOCKER_NETWORK_SUBNET"
  echo "--- Cluster ${name} (index=${index}): pods=${POD_CIDR} svcs=${SVC_CIDR} net=${network} ---"

  if kind get clusters 2>/dev/null | grep -qx "$name"; then
    echo "Cluster ${name} already exists, skipping creation"
  else
    generate_kind_config "$WORKERS" "$POD_CIDR" "$SVC_CIDR" | \
      KIND_EXPERIMENTAL_DOCKER_NETWORK="$network" \
      kind create cluster \
        --name "$name" \
        --kubeconfig "$kubeconfig" \
        --config /dev/stdin
  fi

  load_ovn_image "$name"
  configure_node_sysctl "$name"
}

CIDR_BASE="${CIDR_BASE:-1}"
HUB_NETWORK=$(cluster_network "$HUB_NAME")
create_cluster "$HUB_NAME" "$CIDR_BASE" "$HUB_KUBECONFIG" "$HUB_NETWORK"
for ((i = 1; i <= SPOKE_COUNT; i++)); do
  create_cluster "$(spoke_name "$i")" "$((CIDR_BASE + i))" "$(spoke_kubeconfig "$i")" "$(cluster_network "$(spoke_name "$i")")"
done
echo ""

echo "=== Phase 3: External FRR route reflector ==="

# Deploy FRR on the hub's Docker network, then connect it to each spoke
# network so it can peer with all cluster nodes across L2 domains.
deploy_external_frr "$HUB_KUBECONFIG"

for ((i = 1; i <= SPOKE_COUNT; i++)); do
  spoke_net=$(cluster_network "$(spoke_name "$i")")
  connect_frr_to_network "$spoke_net"
  spoke_ips=()
  while IFS= read -r ip; do
    spoke_ips+=("$ip")
  done < <(get_node_ips "$(spoke_name "$i")" "$spoke_net")
  add_frr_neighbors "${spoke_ips[@]}"
done
echo ""

echo "=== Phase 4: OVN-Kubernetes + FRR-K8s ==="

deploy_ovnk_to_cluster() {
  local name=$1 index=$2 kubeconfig=$3 network=$4

  compute_cidrs "$index"
  install_frr_k8s "$kubeconfig"
  helm_install_ovnk "$name" "$kubeconfig" "$network"
  wait_for_ovnk "$kubeconfig"
  configure_frr_k8s_peering "$kubeconfig" "$name"
}

deploy_ovnk_to_cluster "$HUB_NAME" "$CIDR_BASE" "$HUB_KUBECONFIG" "$HUB_NETWORK"
for ((i = 1; i <= SPOKE_COUNT; i++)); do
  deploy_ovnk_to_cluster "$(spoke_name "$i")" "$((CIDR_BASE + i))" "$(spoke_kubeconfig "$i")" "$(cluster_network "$(spoke_name "$i")")"
done
echo ""

echo "=== Phase 5: Plexus controller ==="
# VTEP CIDRs must cover all cluster Docker network subnets so EVPN
# tunnels can be established across L2 domains.
VTEP_CIDRS=()
compute_cidrs "$CIDR_BASE"
VTEP_CIDRS+=("$DOCKER_NETWORK_SUBNET")
for ((i = 1; i <= SPOKE_COUNT; i++)); do
  compute_cidrs "$((CIDR_BASE + i))"
  VTEP_CIDRS+=("$DOCKER_NETWORK_SUBNET")
done
deploy_plexus_controller "$HUB_NAME" "$HUB_KUBECONFIG" "${VTEP_CIDRS[@]}"
echo ""

echo "=== Phase 6: Spoke cluster Secrets ==="
for ((i = 1; i <= SPOKE_COUNT; i++)); do
  create_spoke_secret "$HUB_KUBECONFIG" "$(spoke_name "$i")" "$i" "$(cluster_network "$(spoke_name "$i")")"
done
echo ""

echo "============================================"
echo "  Multi-cluster Plexus environment is ready"
echo "============================================"
echo ""
echo "Hub cluster:"
echo "  Name:       ${HUB_NAME}"
echo "  KUBECONFIG: ${HUB_KUBECONFIG}"
echo ""
for ((i = 1; i <= SPOKE_COUNT; i++)); do
  echo "Spoke cluster ${i}:"
  echo "  Name:       $(spoke_name "$i")"
  echo "  KUBECONFIG: $(spoke_kubeconfig "$i")"
  echo ""
done
echo "Plexus controller:"
echo "  KUBECONFIG=${HUB_KUBECONFIG} kubectl get pods -n plexus-system"
echo ""
echo "Spoke Secrets:"
echo "  KUBECONFIG=${HUB_KUBECONFIG} kubectl -n plexus-system get secrets -l plexus.io/cluster=true"
