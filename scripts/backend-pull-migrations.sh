#!/usr/bin/env bash
cd "$(dirname "$0")"

POD=$(kubectl get pod -l app=backend -o name -n rootenv-infra | sed 's\pod/\\')

if [ -z "$POD" ]; then
  echo "No backend pod found. Make sure the backend is running."
  exit 1
fi

kubectl cp $POD:/app/pb_migrations ..services/backend/pb_migrations  -n rootenv-infra

if [ $? -ne 0 ]; then
  echo "Failed to pull migrations from the backend pod."
  exit 1
fi

echo "Migrations pulled to ./pb_migrations"

cd -