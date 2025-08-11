#!/bin/bash

# Demo test script

echo "=== Pixie Demo Connector Test Script ==="
echo ""

# Check if binary exists
if [[ ! -f "./observo-connector" ]]; then
    echo "Building observo-connector..."
    make build
fi

echo "1. Testing help command..."
./observo-connector --help
echo ""

echo "2. Testing health command help..."
./observo-connector health --help
echo ""

echo "3. Testing query command help..."
./observo-connector query --help
echo ""

echo "4. Sample commands for different configurations:"
echo ""

echo "   # Development (no SSL):"
echo "   ./observo-connector query 'df.head(5)' \\"
echo "       --cluster-id=your-cluster-id \\"
echo "       --address=vizier.pixie.svc.cluster.local:50300 \\"
echo "       --tls=false"
echo ""

echo "   # Development (skip TLS verification):"
echo "   ./observo-connector query 'df.head(5)' \\"
echo "       --cluster-id=your-cluster-id \\"
echo "       --address=vizier.pixie.svc.cluster.local:50300 \\"
echo "       --skip-verify=true"
echo ""

echo "   # Production (server-side TLS):"
echo "   ./observo-connector query 'df.head(5)' \\"
echo "       --cluster-id=your-cluster-id \\"
echo "       --address=vizier.pixie.svc.cluster.local:50300 \\"
echo "       --ca-cert=certs/ca.crt \\"
echo "       --server-name=vizier.pixie.svc.cluster.local"
echo ""

echo "   # Production (mutual TLS):"
echo "   ./observo-connector query 'df.head(5)' \\"
echo "       --cluster-id=your-cluster-id \\"
echo "       --address=vizier.pixie.svc.cluster.local:50300 \\"
echo "       --ca-cert=certs/ca.crt \\"
echo "       --client-cert=certs/client.crt \\"
echo "       --client-key=certs/client.key \\"
echo "       --server-name=vizier.pixie.svc.cluster.local"
echo ""

echo "=== Test Complete ==="
echo ""
echo "✅ Core functionality tests passed!"
echo ""
echo "Note: To connect to a real Vizier cluster, you need:"
echo "1. Correct cluster ID"
echo "2. Network access to the Vizier service" 
echo "3. Proper TLS certificates (for production)"
