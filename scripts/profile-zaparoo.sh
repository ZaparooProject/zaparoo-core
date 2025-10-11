#!/bin/bash

set -e

if [ -z "$1" ]; then
    echo "Usage: $0 <address:port> [cpu_seconds] [trace_seconds]"
    echo "Example: $0 10.0.0.107:7497"
    echo "Example: $0 10.0.0.107:7497 30 5"
    echo ""
    echo "Defaults (development):"
    echo "  cpu_seconds:   60 seconds"
    echo "  trace_seconds: 10 seconds"
    exit 1
fi

ADDRESS="$1"
CPU_SECONDS="${2:-60}"
TRACE_SECONDS="${3:-10}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="$REPO_ROOT/tmp"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)

mkdir -p "$OUTPUT_DIR"

echo "Zaparoo Profiling Script"
echo "========================"
echo "Target: http://$ADDRESS"
echo "Output: $OUTPUT_DIR"
echo "Timestamp: $TIMESTAMP"
echo "CPU Profile Duration: ${CPU_SECONDS}s"
echo "Trace Duration: ${TRACE_SECONDS}s"
echo ""

BASE_URL="http://$ADDRESS/debug/pprof"

echo "[1/11] Fetching CPU profile (${CPU_SECONDS} seconds)..."
curl -f -o "$OUTPUT_DIR/cpu_${TIMESTAMP}.prof" \
    "$BASE_URL/profile?seconds=${CPU_SECONDS}" 2>/dev/null && \
    echo "✓ Saved to cpu_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch CPU profile"

echo "[2/11] Fetching heap profile..."
curl -f -o "$OUTPUT_DIR/heap_${TIMESTAMP}.prof" \
    "$BASE_URL/heap" 2>/dev/null && \
    echo "✓ Saved to heap_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch heap profile"

echo "[3/11] Fetching goroutine profile..."
curl -f -o "$OUTPUT_DIR/goroutine_${TIMESTAMP}.prof" \
    "$BASE_URL/goroutine" 2>/dev/null && \
    echo "✓ Saved to goroutine_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch goroutine profile"

echo "[4/11] Fetching allocs profile..."
curl -f -o "$OUTPUT_DIR/allocs_${TIMESTAMP}.prof" \
    "$BASE_URL/allocs" 2>/dev/null && \
    echo "✓ Saved to allocs_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch allocs profile"

echo "[5/11] Fetching block profile..."
curl -f -o "$OUTPUT_DIR/block_${TIMESTAMP}.prof" \
    "$BASE_URL/block" 2>/dev/null && \
    echo "✓ Saved to block_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch block profile"

echo "[6/11] Fetching mutex profile..."
curl -f -o "$OUTPUT_DIR/mutex_${TIMESTAMP}.prof" \
    "$BASE_URL/mutex" 2>/dev/null && \
    echo "✓ Saved to mutex_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch mutex profile"

echo "[7/11] Fetching threadcreate profile..."
curl -f -o "$OUTPUT_DIR/threadcreate_${TIMESTAMP}.prof" \
    "$BASE_URL/threadcreate" 2>/dev/null && \
    echo "✓ Saved to threadcreate_${TIMESTAMP}.prof" || \
    echo "✗ Failed to fetch threadcreate profile"

echo "[8/11] Fetching execution trace (${TRACE_SECONDS} seconds)..."
curl -f -o "$OUTPUT_DIR/trace_${TIMESTAMP}.out" \
    "$BASE_URL/trace?seconds=${TRACE_SECONDS}" 2>/dev/null && \
    echo "✓ Saved to trace_${TIMESTAMP}.out" || \
    echo "✗ Failed to fetch trace"

echo "[9/11] Fetching symbol table..."
curl -f -o "$OUTPUT_DIR/symbol_${TIMESTAMP}.txt" \
    "$BASE_URL/symbol" 2>/dev/null && \
    echo "✓ Saved to symbol_${TIMESTAMP}.txt" || \
    echo "✗ Failed to fetch symbol table"

echo "[10/11] Fetching cmdline..."
curl -f -o "$OUTPUT_DIR/cmdline_${TIMESTAMP}.txt" \
    "$BASE_URL/cmdline" 2>/dev/null && \
    echo "✓ Saved to cmdline_${TIMESTAMP}.txt" || \
    echo "✗ Failed to fetch cmdline"

echo "[11/11] Fetching index page..."
curl -f -o "$OUTPUT_DIR/index_${TIMESTAMP}.html" \
    "$BASE_URL/" 2>/dev/null && \
    echo "✓ Saved to index_${TIMESTAMP}.html" || \
    echo "✗ Failed to fetch index"

echo ""
echo "Profiling complete!"
echo ""
echo "Analysis commands:"
echo "  CPU:        go tool pprof $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
echo "  Heap:       go tool pprof $OUTPUT_DIR/heap_${TIMESTAMP}.prof"
echo "  Goroutines: go tool pprof $OUTPUT_DIR/goroutine_${TIMESTAMP}.prof"
echo "  Allocs:     go tool pprof $OUTPUT_DIR/allocs_${TIMESTAMP}.prof"
echo "  Block:      go tool pprof $OUTPUT_DIR/block_${TIMESTAMP}.prof"
echo "  Mutex:      go tool pprof $OUTPUT_DIR/mutex_${TIMESTAMP}.prof"
echo "  Trace:      go tool trace $OUTPUT_DIR/trace_${TIMESTAMP}.out"
echo ""
echo "Web UI:"
echo "  go tool pprof -http=:8080 $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
echo ""
echo "Common analysis patterns:"
echo "  Top allocators: go tool pprof -alloc_space -top $OUTPUT_DIR/heap_${TIMESTAMP}.prof"
echo "  Inuse memory:   go tool pprof -inuse_space -top $OUTPUT_DIR/heap_${TIMESTAMP}.prof"
echo "  Flame graph:    go tool pprof -http=:8080 $OUTPUT_DIR/cpu_${TIMESTAMP}.prof"
echo ""
echo "Note: Block and mutex profiles may be empty unless runtime.SetBlockProfileRate()"
echo "      and runtime.SetMutexProfileFraction() are called in the application."
