#!/bin/bash

# Demo connector deployment script for Kubernetes

# Set variables
NAMESPACE="pl"
APP_NAME="observo-connector"
IMAGE_NAME="observo-connector:latest"

echo "Building Docker image..."
cat << 'EOF' > Dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o observo-connector ./cmd

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/observo-connector .
CMD ["./observo-connector"]
EOF

# Build image
docker build -t $IMAGE_NAME .

echo "Creating Kubernetes manifests..."

# Create namespace if it doesn't exist
kubectl create namespace $NAMESPACE --dry-run=client -o yaml | kubectl apply -f -

# Create deployment manifest
cat << EOF > deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $APP_NAME
  namespace: $NAMESPACE
  labels:
    app: $APP_NAME
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $APP_NAME
  template:
    metadata:
      labels:
        app: $APP_NAME
    spec:
      containers:
      - name: $APP_NAME
        image: $IMAGE_NAME
        imagePullPolicy: Never
        command: ["/bin/sh"]
        args: ["-c", "while true; do sleep 3600; done"]
        env:
        - name: CLUSTER_ID
          value: "your-cluster-id"
        - name: ADDRESS
          value: "vizier.pixie.svc.cluster.local:51400"
        volumeMounts:
        - name: certs
          mountPath: /etc/ssl/certs
          readOnly: true
        - name: config
          mountPath: /etc/observo-connector
          readOnly: true
      volumes:
      - name: certs
        secret:
          secretName: observo-connector-certs
          optional: true
      - name: config
        configMap:
          name: observo-connector-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: observo-connector-config
  namespace: $NAMESPACE
data:
  config.yaml: |
    address: "vizier.pixie.svc.cluster.local:51400"
    cluster-id: "your-cluster-id"
    skip-verify: true
---
apiVersion: v1
kind: Secret
metadata:
  name: observo-connector-certs
  namespace: $NAMESPACE
type: Opaque
data:
  # Base64 encoded certificates
  # To create: echo -n "cert-content" | base64
  ca.crt: ""
  client.crt: ""
  client.key: ""
EOF

echo "Deploying to Kubernetes..."
kubectl apply -f deployment.yaml

echo "Deployment complete!"
echo ""
echo "To test the connector:"
echo "kubectl exec -it deployment/$APP_NAME -n $NAMESPACE -- ./observo-connector health --cluster-id=your-cluster-id --skip-verify=true"
echo ""
echo "To run a query:"
echo "kubectl exec -it deployment/$APP_NAME -n $NAMESPACE -- ./observo-connector query 'df.head(5)' --cluster-id=your-cluster-id --skip-verify=true"
echo ""
echo "To view logs:"
echo "kubectl logs deployment/$APP_NAME -n $NAMESPACE"
echo ""
echo "To clean up:"
echo "kubectl delete deployment $APP_NAME -n $NAMESPACE"
echo "kubectl delete configmap observo-connector-config -n $NAMESPACE"
echo "kubectl delete secret observo-connector-certs -n $NAMESPACE"
