// Package runtimecfg tunes the Go runtime to the container it runs in.
package runtimecfg

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// memoryShare is the part of the container memory given to the Go heap limit:
// the rest covers the stacks, the runtime and the non-heap memory.
const memoryShare = 0.85

// SetMemoryLimit sets the soft memory limit of the runtime (GOMEMLIMIT) to 85 %
// of the memory limit of the container (cgroup v2 or v1), so the GC works harder
// before the container is killed for exceeding its memory. An explicit GOMEMLIMIT
// wins. It returns the limit set, 0 when none.
func SetMemoryLimit() int64 {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0
	}
	limit := cgroupMemoryLimit("/proc/self/cgroup", "/sys/fs/cgroup")
	if limit <= 0 {
		return 0
	}
	l := int64(float64(limit) * memoryShare)
	debug.SetMemoryLimit(l)
	return l
}

// cgroupMemoryLimit reads the memory limit of the cgroup of this process
// (0 when unknown or unlimited).
func cgroupMemoryLimit(procCgroup, root string) int64 {
	f, err := os.Open(procCgroup)
	if err != nil {
		return 0
	}
	defer f.Close()
	var candidates []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// hierarchy-ID:controller-list:cgroup-path
		parts := strings.SplitN(sc.Text(), ":", 3)
		if len(parts) != 3 {
			continue
		}
		switch {
		case parts[0] == "0" && parts[1] == "": // v2
			candidates = append(candidates, filepath.Join(root, parts[2], "memory.max"), filepath.Join(root, "memory.max"))
		case hasController(parts[1], "memory"): // v1
			candidates = append(candidates,
				filepath.Join(root, "memory", parts[2], "memory.limit_in_bytes"),
				filepath.Join(root, "memory", "memory.limit_in_bytes"))
		}
	}
	for _, c := range candidates {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(b))
		if s == "max" {
			return 0
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 || v >= 1<<62 { // v1 "unlimited" is a huge page-aligned value
			return 0
		}
		return v
	}
	return 0
}

func hasController(list, name string) bool {
	for _, c := range strings.Split(list, ",") {
		if c == name {
			return true
		}
	}
	return false
}
