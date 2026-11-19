#!/bin/bash
# Run the cross-compiled benchmark binaries on a MiSTer and collect the results.
#
# The binaries run detached on the device, so a dropped SSH session does not
# end a run that takes hours, and with TMPDIR on the SD card: /tmp is tmpfs
# there, where a database benchmark would never pay for a real write.
#
# Usage: bench-run.sh <mister-ip> <output-file>
# Environment:
#   BENCH_COUNT     -test.count for every binary (default 3)
#   BENCH_PATTERN   -test.bench for binaries without their own pattern (default .)
#   BENCH_TIMEOUT   -test.timeout for every binary (default 300m)
#   BENCH_PATTERNS  file of "<binary> <pattern>" lines (default bench-patterns.txt
#                   beside this script); set it to /dev/null to run everything
#   BENCH_BIN_DIR   directory of *.test binaries (default _build/bench_arm)

set -euo pipefail

if [ $# -ne 2 ]; then
    echo "Usage: $0 <mister-ip> <output-file>" >&2
    exit 1
fi

ip="$1"
out="$2"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
count="${BENCH_COUNT:-3}"
default_pattern="${BENCH_PATTERN:-.}"
timeout="${BENCH_TIMEOUT:-300m}"
patterns="${BENCH_PATTERNS:-$script_dir/bench-patterns.txt}"
bin_dir="${BENCH_BIN_DIR:-_build/bench_arm}"
remote=/media/fat/zaparoo-bench
ssh_opts=(-o ConnectTimeout=20 -o ServerAliveInterval=30 -o ServerAliveCountMax=4)

shopt -s nullglob
bins=("$bin_dir"/*.test)
if [ ${#bins[@]} -eq 0 ]; then
    echo "no benchmark binaries in $bin_dir; run task mister:bench-build first" >&2
    exit 1
fi

# A patterns file that cannot be read must stop the run: falling back to the
# default pattern would start the whole suite, which takes hours on the card.
if [ ! -r "$patterns" ]; then
    echo "cannot read benchmark patterns file $patterns" >&2
    exit 1
fi

pattern_for() {
    local found
    if ! found="$(awk -v name="$1" '$1 == name { sub(/^[^ \t]+[ \t]+/, ""); print; exit }' "$patterns")"; then
        echo "failed to read a pattern for $1 from $patterns" >&2
        return 1
    fi
    printf '%s' "${found:-$default_pattern}"
}

runner="$(mktemp)"
trap 'rm -f "$runner"' EXIT
{
    echo '#!/bin/sh'
    echo "cd $remote || exit 1"
    echo 'mkdir -p tmp out'
    echo 'run() {'
    echo "    TMPDIR=$remote/tmp ./\"\$1\" -test.run='^\$' -test.bench=\"\$2\" -test.benchmem \\"
    echo "        -test.count=$count -test.timeout=$timeout > \"out/\$1.txt\" 2> \"out/\$1.err\""
    echo '    echo "$1 $?" >> out/status'
    echo '    rm -rf tmp/*'
    echo '}'
    for bin in "${bins[@]}"; do
        name="$(basename "$bin")"
        pattern="$(pattern_for "$name")"
        printf "run %q %q\n" "$name" "$pattern"
    done
    echo 'touch out/done'
} > "$runner"

# The run is detached, so one started earlier may still be going after its
# observer stopped. Starting again would delete its binaries, output and
# TMPDIR; refuse instead. A finished or abandoned run leaves no process.
running="$(ssh "${ssh_opts[@]}" "root@$ip" "for p in /proc/[0-9]*; do
    tr '\\0' ' ' < \$p/cmdline 2> /dev/null | grep -q '^sh $remote/run.sh' && echo \${p#/proc/}
done; true")"
if [ -n "$running" ]; then
    echo "a benchmark run is already active on $ip (pid $running);" \
        "wait for it, or stop it there before starting another" >&2
    exit 1
fi
ssh "${ssh_opts[@]}" "root@$ip" "rm -rf $remote && mkdir -p $remote"
scp "${ssh_opts[@]}" "${bins[@]}" "root@$ip:$remote/"
scp "${ssh_opts[@]}" "$runner" "root@$ip:$remote/run.sh"
ssh "${ssh_opts[@]}" "root@$ip" "nohup sh $remote/run.sh > /dev/null 2>&1 &"

echo "benchmarks running on $ip; this can take hours" >&2
last=""
until ssh "${ssh_opts[@]}" "root@$ip" "test -e $remote/out/done" 2> /dev/null; do
    current="$(ssh "${ssh_opts[@]}" "root@$ip" "tail -n 1 $remote/out/status" 2> /dev/null || true)"
    if [ -n "$current" ] && [ "$current" != "$last" ]; then
        echo "finished: $current" >&2
        last="$current"
    fi
    sleep 60
done

results="$(mktemp -d)"
trap 'rm -rf "$runner" "$results"' EXIT
scp "${ssh_opts[@]}" "root@$ip:$remote/out/*" "$results/"
for bin in "${bins[@]}"; do
    cat "$results/$(basename "$bin").txt"
done | grep -E "^(Benchmark.* ns/op|goos:|goarch:|pkg:|cpu:)" | tee "$out"

failed="$(awk '$2 != 0 { print $1 }' "$results/status")"
ssh "${ssh_opts[@]}" "root@$ip" "rm -rf $remote"
if [ -n "$failed" ]; then
    for name in $failed; do
        echo "--- $name failed:" >&2
        tail -n 20 "$results/$name.err" >&2
    done
    exit 1
fi
