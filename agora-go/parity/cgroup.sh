#!/bin/bash
# Confine a process tree to N CPUs and M MB of RAM (cgroup v1), to emulate a
# Scalingo container size during load tests.
#   parity/cgroup.sh <name> <cpus (e.g. 1 or 0.5)> <memMB> <pid>
set -euo pipefail
name=$1; cpus=$2; mem=$3; pid=$4
for c in cpu memory; do mkdir -p /sys/fs/cgroup/$c/$name; done
quota=$(python3 -c "print(int(float('$cpus')*100000))")
echo 100000 > /sys/fs/cgroup/cpu/$name/cpu.cfs_period_us
echo $quota > /sys/fs/cgroup/cpu/$name/cpu.cfs_quota_us
echo $((mem*1024*1024)) > /sys/fs/cgroup/memory/$name/memory.limit_in_bytes
for t in $(ls /proc/$pid/task); do
  echo $t > /sys/fs/cgroup/cpu/$name/tasks
  echo $t > /sys/fs/cgroup/memory/$name/tasks
done
echo "pid $pid → cgroup $name ($cpus CPU, ${mem}MB)"
