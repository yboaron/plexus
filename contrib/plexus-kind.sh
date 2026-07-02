#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
#
# Create a single KIND cluster with OVN-Kubernetes + EVPN for Plexus development.
#
# Usage:
#   contrib/plexus-kind.sh [OPTIONS]
#
# Options:
#   --name NAME        Cluster name (default: plexus)
#   --workers N        Number of worker nodes (default: 1)
#   --skip-build       Reuse existing OVN image
#   --force-build      Rebuild OVN image even if it exists
#   --delete           Tear down the cluster and exit

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/plexus-kind-common.sh"

CLUSTER_NAME="plexus"
WORKERS=1
SKIP_BUILD=false
DELETE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --name)       CLUSTER_NAME="$2"; shift 2 ;;
    --workers)    WORKERS="$2"; shift 2 ;;
    --skip-build) SKIP_BUILD=true; shift ;;
    --force-build) FORCE_BUILD=true; shift ;;
    --delete)     DELETE=true; shift ;;
    *)            echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done

KUBECONFIG_FILE="${HOME}/${CLUSTER_NAME}.conf"
export KUBECONFIG="$KUBECONFIG_FILE"

DOCKER_NET="plexus-${CLUSTER_NAME}"

if [ "$DELETE" = true ]; then
  echo "=== Deleting cluster ${CLUSTER_NAME} ==="
  kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
  cleanup_external_frr
  delete_docker_network "$DOCKER_NET"
  rm -f "$KUBECONFIG_FILE"
  echo "Done."
  exit 0
fi

resolve_ovn_kubernetes_path
echo "Using OVN-Kubernetes from: ${OVN_KUBERNETES_PATH}"

if [ "$SKIP_BUILD" = true ]; then
  OVN_IMAGE="localhost/ovn-daemonset-fedora:dev"
  PLEXUS_IMAGE="localhost/plexus-controller:dev"
  echo "Skipping image builds, using existing images"
else
  build_ovn_image
  build_plexus_image
fi

compute_cidrs "${CIDR_INDEX:-0}"
echo "Pod CIDR: ${POD_CIDR}, Service CIDR: ${SVC_CIDR}"

echo "=== Creating KIND cluster: ${CLUSTER_NAME} ==="
create_docker_network "$DOCKER_NET" "$DOCKER_NETWORK_SUBNET"
kind_config=$(generate_kind_config "$WORKERS" "$POD_CIDR" "$SVC_CIDR")

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "Cluster ${CLUSTER_NAME} already exists, skipping creation"
else
  echo "$kind_config" | \
    KIND_EXPERIMENTAL_DOCKER_NETWORK="$DOCKER_NET" \
    kind create cluster \
      --name "$CLUSTER_NAME" \
      --kubeconfig "$KUBECONFIG_FILE" \
      --config /dev/stdin
fi

load_ovn_image "$CLUSTER_NAME"
configure_node_sysctl "$CLUSTER_NAME"

deploy_external_frr "$KUBECONFIG_FILE"
install_frr_k8s "$KUBECONFIG_FILE"
helm_install_ovnk "$CLUSTER_NAME" "$KUBECONFIG_FILE" "$DOCKER_NET"
wait_for_ovnk "$KUBECONFIG_FILE"
configure_frr_k8s_peering "$KUBECONFIG_FILE" "$CLUSTER_NAME"
deploy_plexus_controller "$CLUSTER_NAME" "$KUBECONFIG_FILE" "$DOCKER_NETWORK_SUBNET"

echo ""
echo "=== Cluster ${CLUSTER_NAME} is ready ==="
echo "  KUBECONFIG=${KUBECONFIG_FILE}"
echo "  kubectl --kubeconfig=${KUBECONFIG_FILE} get nodes"
echo "  kubectl --kubeconfig=${KUBECONFIG_FILE} get pods -n plexus-system"
