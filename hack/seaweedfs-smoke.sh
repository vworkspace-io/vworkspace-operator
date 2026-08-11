#!/usr/bin/env bash
# Cluster-side E2E smoke for managed SeaweedFS (P10-T006).
#
# Prerequisites (kind / dev-real):
#   - vworkspace-operator chart with seaweedfsOperator.enabled (P10-T002)
#   - ApplicationInstance CRD + controller running
#   - Default StorageClass for volume PVCs
#
# Deploys a seaweedfs ApplicationInstance, waits for Ready + S3 gateway,
# provisions smoke IAM (S3Identity/S3Credentials), and runs in-cluster
# `aws s3 ls` against the path-style endpoint on port 8333.
#
# Usage:
#   ./hack/seaweedfs-smoke.sh
#   ./hack/seaweedfs-smoke.sh --skip-deploy --name seaweedfs-dev --namespace seaweedfs
#   ./hack/seaweedfs-smoke.sh --cleanup
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

INSTANCE_NAME="${VWORKSPACE_SEAWEEDFS_NAME:-seaweedfs-smoke}"
NAMESPACE="${VWORKSPACE_SEAWEEDFS_NAMESPACE:-seaweedfs}"
TIMEOUT="${VWORKSPACE_SEAWEEDFS_TIMEOUT:-900}"
VOLUME_SIZE="${VWORKSPACE_SEAWEEDFS_VOLUME_SIZE:-1Gi}"
OPERATOR_NS="${VWORKSPACE_OPERATOR_NAMESPACE:-vworkspace-system}"
SKIP_DEPLOY=0
CLEANUP=0
SKIP_IAM=0

for arg in "$@"; do
  case "$arg" in
    --name=*) INSTANCE_NAME="${arg#*=}" ;;
    --namespace=*) NAMESPACE="${arg#*=}" ;;
    --skip-deploy) SKIP_DEPLOY=1 ;;
    --skip-iam) SKIP_IAM=1 ;;
    --cleanup) CLEANUP=1 ;;
    -h|--help)
      sed -n '2,22p' "$0"
      exit 0
      ;;
    *)
      echo "Unknown option: $arg" >&2
      exit 1
      ;;
  esac
done

S3_SERVICE="${INSTANCE_NAME}-s3"
S3_URL="http://${S3_SERVICE}.${NAMESPACE}.svc:8333"
IAM_IDENTITY="${INSTANCE_NAME}-smoke"
IAM_CREDS="${INSTANCE_NAME}-smoke-creds"
IAM_SECRET="${INSTANCE_NAME}-smoke-s3-secret"
IAM_POLICY="${INSTANCE_NAME}-smoke-rw"
IAM_BINDING="${INSTANCE_NAME}-smoke-binding"

log() {
  echo "[seaweedfs-smoke] $*"
}

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

require_cmd kubectl

if [[ "$CLEANUP" -eq 1 ]]; then
  log "cleaning up smoke resources in ${NAMESPACE}"
  kubectl delete applicationinstance "${INSTANCE_NAME}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete seaweed "${INSTANCE_NAME}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete s3policybinding "${IAM_BINDING}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete s3policy "${IAM_POLICY}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete s3credentials "${IAM_CREDS}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete s3identity "${IAM_IDENTITY}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  kubectl delete secret "${IAM_SECRET}" -n "${NAMESPACE}" --ignore-not-found --wait=false
  exit 0
fi

check_prerequisites() {
  log "checking prerequisites"

  if ! kubectl get crd applicationinstances.apps.vworkspace.io >/dev/null 2>&1; then
    fail "ApplicationInstance CRD missing — install vworkspace-operator first"
  fi

  if ! kubectl get crd seaweeds.seaweed.seaweedfs.com >/dev/null 2>&1; then
    fail "$(cat <<EOF
Seaweed CRD missing (tier-1 seaweedfs-operator bundle not installed).
Install the operator chart with values-kind.yaml (P10-T002), then retry:
  helm upgrade --install vworkspace-operator ./charts/vworkspace-operator \\
    -n ${OPERATOR_NS} --create-namespace \\
    -f charts/vworkspace-operator/values-kind.yaml
Verify: kubectl get crd seaweeds.seaweed.seaweedfs.com
EOF
)"
  fi

  if ! kubectl get crd s3credentials.seaweed.seaweedfs.com >/dev/null 2>&1; then
    fail "S3Credentials CRD missing — seaweedfs-operator CRDs not established"
  fi

  if ! kubectl -n "${OPERATOR_NS}" get deploy -l app.kubernetes.io/name=vworkspace-operator >/dev/null 2>&1; then
    if ! kubectl -n "${OPERATOR_NS}" get deploy vworkspace-operator >/dev/null 2>&1; then
      fail "vworkspace-operator deployment not found in namespace ${OPERATOR_NS}"
    fi
  fi

  if ! kubectl -n seaweedfs-operator get deploy seaweedfs-operator >/dev/null 2>&1; then
    fail "$(cat <<EOF
seaweedfs-operator controller not found in namespace seaweedfs-operator.
Enable seaweedfsOperator.enabled on the vworkspace-operator chart (P10-T002).
EOF
)"
  fi

  local sc
  sc="$(kubectl get storageclass -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}' 2>/dev/null || true)"
  if [[ -z "${sc}" ]]; then
    sc="$(kubectl get storageclass -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  fi
  if [[ -z "${sc}" ]]; then
    fail "no StorageClass found — Seaweed volume PVCs will stay Pending"
  fi
  log "using StorageClass: ${sc}"
}

apply_application_instance() {
  log "creating namespace ${NAMESPACE} (if needed)"
  kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

  log "applying ApplicationInstance ${INSTANCE_NAME} in ${NAMESPACE}"
  cat <<YAML | kubectl apply -f -
apiVersion: apps.vworkspace.io/v1alpha1
kind: ApplicationInstance
metadata:
  name: ${INSTANCE_NAME}
  namespace: ${NAMESPACE}
spec:
  appRef:
    catalogId: seaweedfs
  chart:
    sourceType: helm
    url: https://raw.githubusercontent.com/vworkspace-io/vworkspace-server/main/charts/
    name: seaweedfs
    version: "0.1.0"
  release:
    name: ${INSTANCE_NAME}
    namespace: ${NAMESPACE}
  values:
    source: inline
    inline:
      master:
        replicas: 1
      volume:
        replicas: 1
        requests:
          storage: ${VOLUME_SIZE}
      filer:
        replicas: 1
      s3:
        replicas: 1
YAML
}

wait_for_application_ready() {
  log "waiting for ApplicationInstance/${INSTANCE_NAME} Ready=True (timeout ${TIMEOUT}s)"
  local deadline=$((SECONDS + TIMEOUT))
  while (( SECONDS < deadline )); do
    local ready reason message
    ready="$(kubectl get applicationinstance "${INSTANCE_NAME}" -n "${NAMESPACE}" \
      -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.status}{end}' 2>/dev/null || true)"
    reason="$(kubectl get applicationinstance "${INSTANCE_NAME}" -n "${NAMESPACE}" \
      -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.reason}{end}' 2>/dev/null || true)"
    message="$(kubectl get applicationinstance "${INSTANCE_NAME}" -n "${NAMESPACE}" \
      -o jsonpath='{range .status.conditions[?(@.type=="Ready")]}{.message}{end}' 2>/dev/null || true)"
    if [[ "${ready}" == "True" ]]; then
      log "ApplicationInstance Ready (${reason})"
      return 0
    fi
    if [[ "${ready}" == "False" && "${reason}" == "SeaweedFailed" ]]; then
      kubectl get seaweed "${INSTANCE_NAME}" -n "${NAMESPACE}" -o yaml >&2 || true
      fail "ApplicationInstance failed: ${message:-Seaweed reconcile error}"
    fi
    sleep 5
  done
  kubectl describe applicationinstance "${INSTANCE_NAME}" -n "${NAMESPACE}" >&2 || true
  kubectl describe seaweed "${INSTANCE_NAME}" -n "${NAMESPACE}" >&2 || true
  fail "timed out waiting for ApplicationInstance Ready=True"
}

wait_for_seaweed_ready() {
  log "waiting for Seaweed/${INSTANCE_NAME} Ready condition"
  if ! kubectl wait --for=condition=Ready "seaweed/${INSTANCE_NAME}" -n "${NAMESPACE}" --timeout="${TIMEOUT}s" 2>/dev/null; then
    kubectl get pods -n "${NAMESPACE}" -l "app.kubernetes.io/instance=${INSTANCE_NAME}" >&2 || true
    kubectl get pvc -n "${NAMESPACE}" >&2 || true
    fail "$(cat <<EOF
Seaweed CR did not become Ready within ${TIMEOUT}s.
Common causes: PVC Pending (no/default StorageClass), seaweedfs-operator not running,
or insufficient cluster resources. Inspect:
  kubectl describe seaweed ${INSTANCE_NAME} -n ${NAMESPACE}
  kubectl get pvc -n ${NAMESPACE}
EOF
)"
  fi
}

wait_for_s3_service() {
  log "waiting for Service/${S3_SERVICE} endpoints"
  local deadline=$((SECONDS + 120))
  while (( SECONDS < deadline )); do
    if kubectl get svc "${S3_SERVICE}" -n "${NAMESPACE}" >/dev/null 2>&1; then
      local eps
      eps="$(kubectl get endpoints "${S3_SERVICE}" -n "${NAMESPACE}" -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null || true)"
      if [[ -n "${eps}" ]]; then
        log "S3 gateway endpoint ready at ${S3_URL}"
        return 0
      fi
    fi
    sleep 3
  done
  fail "S3 gateway Service ${S3_SERVICE} has no ready endpoints — check Seaweed spec.s3 and operator logs"
}

apply_smoke_iam() {
  if [[ "$SKIP_IAM" -eq 1 ]]; then
    log "skipping IAM provisioning (--skip-iam)"
    return 0
  fi
  log "provisioning smoke S3 identity, credentials, and rw-all policy"
  cat <<YAML | kubectl apply -f -
apiVersion: seaweed.seaweedfs.com/v1
kind: S3Identity
metadata:
  name: ${IAM_IDENTITY}
  namespace: ${NAMESPACE}
spec:
  seaweedRef:
    name: ${INSTANCE_NAME}
---
apiVersion: seaweed.seaweedfs.com/v1
kind: S3Credentials
metadata:
  name: ${IAM_CREDS}
  namespace: ${NAMESPACE}
spec:
  seaweedRef:
    name: ${INSTANCE_NAME}
  identityRef:
    name: ${IAM_IDENTITY}
  secretRef:
    name: ${IAM_SECRET}
---
apiVersion: seaweed.seaweedfs.com/v1
kind: S3Policy
metadata:
  name: ${IAM_POLICY}
  namespace: ${NAMESPACE}
spec:
  seaweedRef:
    name: ${INSTANCE_NAME}
  statements:
    - effect: Allow
      actions:
        - "*"
      resources:
        - "*"
---
apiVersion: seaweed.seaweedfs.com/v1
kind: S3PolicyBinding
metadata:
  name: ${IAM_BINDING}
  namespace: ${NAMESPACE}
spec:
  seaweedRef:
    name: ${INSTANCE_NAME}
  policyRef:
    name: ${IAM_POLICY}
  subjects:
    - kind: S3Identity
      name: ${IAM_IDENTITY}
YAML

  log "waiting for S3Credentials/${IAM_CREDS} secret ${IAM_SECRET}"
  local deadline=$((SECONDS + 180))
  while (( SECONDS < deadline )); do
    if kubectl get secret "${IAM_SECRET}" -n "${NAMESPACE}" >/dev/null 2>&1; then
      if [[ -n "$(kubectl get secret "${IAM_SECRET}" -n "${NAMESPACE}" -o jsonpath='{.data.secretKey}' 2>/dev/null || true)" ]]; then
        return 0
      fi
    fi
    sleep 3
  done
  fail "timed out waiting for generated S3 credentials secret ${IAM_SECRET}"
}

read_smoke_credentials() {
  SMOKE_ACCESS_KEY="$(kubectl get secret "${IAM_SECRET}" -n "${NAMESPACE}" -o jsonpath='{.data.accessKey}' | base64 -d)"
  SMOKE_SECRET_KEY="$(kubectl get secret "${IAM_SECRET}" -n "${NAMESPACE}" -o jsonpath='{.data.secretKey}' | base64 -d)"
  if [[ -z "${SMOKE_ACCESS_KEY}" || -z "${SMOKE_SECRET_KEY}" ]]; then
    fail "S3 credentials secret ${IAM_SECRET} is missing accessKey/secretKey"
  fi
}

run_s3_list_smoke() {
  log "running in-cluster aws s3 ls against ${S3_URL}"
  kubectl run "seaweedfs-s3-smoke-${RANDOM}" \
    --rm --restart=Never \
    --namespace="${NAMESPACE}" \
    --image=amazon/aws-cli:2.22.12 \
    --env="AWS_ACCESS_KEY_ID=${SMOKE_ACCESS_KEY}" \
    --env="AWS_SECRET_ACCESS_KEY=${SMOKE_SECRET_KEY}" \
    --env="AWS_DEFAULT_REGION=us-east-1" \
    --command -- \
    aws s3 ls --endpoint-url "${S3_URL}"
  log "S3 list-bucket smoke succeeded"
}

check_prerequisites

if [[ "$SKIP_DEPLOY" -eq 0 ]]; then
  apply_application_instance
else
  log "skipping ApplicationInstance apply (--skip-deploy)"
fi

wait_for_seaweed_ready
wait_for_application_ready
wait_for_s3_service
apply_smoke_iam
read_smoke_credentials
run_s3_list_smoke

log "SeaweedFS catalog cluster smoke passed (instance=${INSTANCE_NAME}, s3=${S3_URL})"
