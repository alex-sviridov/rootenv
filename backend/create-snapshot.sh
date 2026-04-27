#!/usr/bin/env bash
cd "$(dirname "$0")"

POD=$(kubectl get pod -l app=backend -o name -n rootenv-infra | sed 's\pod/\\')

if [ -z "$POD" ]; then
  echo "No backend pod found. Make sure the backend is running."
  exit 1
fi

kubectl exec $POD -n rootenv-infra -- sh -c "echo y | /app/pocketbase migrate collections"

if [ $? -ne 0 ]; then
  echo "Failed to pull migrations from the backend pod."
  exit 1
fi

echo "Migration snapshot created in the backend pod."

cd -