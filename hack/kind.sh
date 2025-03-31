#!/usr/bin/env bash
set -o pipefail

print_usage() {
cat <<-EOF
Usage: kind.sh
- Run a kind cluster in a docker container
    kind.sh
EOF
exit 101
}

setup_kind() {
    KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME:-apache}

    # Location of this script
    DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )"

    if [[ $(command -v kind) == "" ]]; then
      echo -e "\e[33mPlease install 'kind'\e[0m"
      exit 1
    fi

    # Create a new cluster if there is not already one installed
    if [[ $(kind get clusters) == "" ]]; then
cat <<EOF | kind create cluster --name "$KIND_CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
EOF
    fi

    # Wait for the local-path-provisioner to stand up ...
    LABEL=app=local-path-provisioner
    source $DIR/pod-ready-wait.sh
}

run_kind() {
    setup_kind
}

run_kind 
