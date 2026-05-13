#!/bin/bash
# Sparkmon cluster health check
# Usage: ./health-check.sh spark-01,spark-02
#        ./health-check.sh "me@192.168.1.101,me@192.168.1.102"
#        ./health-check.sh 192.168.1.101:9100,192.168.1.102:9400

set -e

NODES=""
for arg in "$@"; do
    NODES="$NODES,$arg"
done
NODES=${NODES#,}

if [ -z "$NODES" ]; then
    echo "Usage: $0 <node1,node2,...>"
    echo "  Examples:"
    echo "    $0 spark-01,spark-02"
    echo "    $0 me@192.168.1.101,me@192.168.1.102"
    echo "    $0 192.168.1.101:9100,192.168.1.102:9400"
    exit 1
fi

echo "Checking Sparkmon health on nodes: $NODES"
echo ""

check_node() {
    local node=$1
    local port=$2
    local timeout=3
    
    echo "[$node:$port] TCP check"
    
    # Try to connect
    if timeout $timeout bash -c "echo > /dev/tcp/$node/$port" 2>/dev/null; then
        echo "[$node:$port] TCP connection OK"
        
        # Check if metrics are responding
        if curl -s --max-etime 2 "http://$node:$port/metrics" > /dev/null 2>&1; then
            echo "[$node:$port] Metrics endpoint responding"
        else
            echo "[$node:$port] TCP OK but metrics not responding"
            echo "  Exporters might not be running. Try: sparkmon deploy $node"
        fi
    else
        echo "[$node:$port] Cannot connect (exporters not running or firewall blocking)"
        echo "  Run: sparkmon deploy $node"
    fi
}

check_gpu() {
    local node=$1
    local port=9400
    
    echo ""
    echo "[$node:$port] GPU metrics"
    
    if curl -s --max-etime 2 "http://$node:$port/metrics" | grep -q "DCGM"; then
        echo "[$node:$port] GPU metrics available"
        echo "  GPU utilization: $(curl -s "http://$node:$port/metrics" | grep "DCGM_FI_DEV_GPU_UTIL" | head -1 | cut -d' ' -f2 || echo "N/A")"
    else
        echo "[$node:$port] GPU metrics not available"
        echo "  dcgm-exporter might not be running"
    fi
}

check_docker() {
    local node=$1
    echo ""
    echo "[$node] Docker + GPU support"
    
    if ssh -o ConnectTimeout=2 "$node" docker --version > /dev/null 2>&1; then
        echo "[$node] Docker is installed"
        if ssh -o ConnectTimeout=2 "$node" "docker run --rm --gpus all ubuntu nvidia-smi" > /dev/null 2>&1; then
            echo "[$node] NVIDIA Container Toolkit working"
        else
            echo "[$node] Docker works but GPU containers don't"
            echo "  Install NVIDIA Container Toolkit"
        fi
    else
        echo "[$node] Docker not installed"
    fi
}

# Check all provided nodes
IFS=',' read -ra NODES_ARRAY <<< "$NODES"
for node_spec in "${NODES_ARRAY[@]}"; do
    IFS=':' read -ra HOST_PORT <<< "$node_spec"
    HOST=${HOST_PORT[0]}
    PORT=${HOST_PORT[1]:-9100}
    
    check_node "$HOST" "$PORT"
    
    # Only check GPU if it's a standard Spark node (has colons implies custom ports)
    if [[ "$node_spec" == *:* ]]; then
        check_gpu "$HOST"
        check_docker "$HOST"
    fi
    
    echo ""
done

echo "Health check complete"
echo ""
echo "Next steps:"
echo "  - If nodes are red: sparkmon deploy user@node1,user@node2"
echo "  - Once green: sparkmon up"
echo "  - For help: sparkmon --help"