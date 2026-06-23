#!/usr/bin/env bash
# Deploy event-fanout to DOKS with Managed Postgres + Managed Redis (Streams).
#
# Usage:
#   export DATABASE_URL=postgres://...
#   export REDIS_URL=rediss://...
#   ./scripts/deploy-doks.sh

set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-event-fanout-cluster}"
NAMESPACE="${NAMESPACE:-event-fanout}"
RELEASE_NAME="${RELEASE_NAME:-event-fanout}"
IMAGE_REPO="${IMAGE_REPO:-ghcr.io/shwetaudacious/event-fanout}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

cd "$(dirname "$0")/.."

if [[ -z "${DATABASE_URL:-}" || -z "${REDIS_URL:-}" ]]; then
  echo "ERROR: set DATABASE_URL and REDIS_URL (run scripts/provision-digitalocean.sh first)" >&2
  exit 1
fi

command -v doctl >/dev/null || { echo "ERROR: install doctl"; exit 1; }
command -v helm >/dev/null || { echo "ERROR: install helm"; exit 1; }
command -v kubectl >/dev/null || { echo "ERROR: install kubectl"; exit 1; }

doctl kubernetes cluster kubeconfig save "$CLUSTER_NAME"

echo "[deploy] helm upgrade --install (includes in-cluster migration job)"
helm upgrade --install "$RELEASE_NAME" ./helm/eventfanout \
  --namespace "$NAMESPACE" \
  --create-namespace \
  -f ./helm/eventfanout/values-doks.yaml \
  --set "image.repository=$IMAGE_REPO" \
  --set "image.tag=$IMAGE_TAG" \
  --set "secrets.databaseURL=$DATABASE_URL" \
  --set "secrets.redisURL=$REDIS_URL"

kubectl rollout status "deployment/${RELEASE_NAME}-server" -n "$NAMESPACE" --timeout=180s
kubectl rollout status "deployment/${RELEASE_NAME}-worker" -n "$NAMESPACE" --timeout=180s

echo "[deploy] done"
kubectl get pods,svc -n "$NAMESPACE"
