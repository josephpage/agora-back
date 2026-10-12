package runtimecfg

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCgroupMemoryLimit(t *testing.T) {
	for _, tc := range []struct {
		name, cgroup string
		files        map[string]string
		want         int64
	}{
		{"v2", "0::/app\n", map[string]string{"app/memory.max": "536870912\n"}, 536870912},
		{"v2 max", "0::/app\n", map[string]string{"app/memory.max": "max\n"}, 0},
		{"v2 namespaced root", "0::/\n", map[string]string{"memory.max": "1073741824\n"}, 1073741824},
		{"v1", "9:cpu,cpuacct:/x\n4:memory:/load-go\n", map[string]string{"memory/load-go/memory.limit_in_bytes": "1073741824\n"}, 1073741824},
		{"v1 unlimited", "4:memory:/\n", map[string]string{"memory/memory.limit_in_bytes": "9223372036854771712\n"}, 0},
		{"v1 namespaced", "4:memory:/docker/abc\n", map[string]string{"memory/memory.limit_in_bytes": "2147483648\n"}, 2147483648},
		{"none", "1:name=systemd:/\n", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			write(t, filepath.Join(dir, "self-cgroup"), tc.cgroup)
			for p, s := range tc.files {
				write(t, filepath.Join(dir, "fs", p), s)
			}
			if got := cgroupMemoryLimit(filepath.Join(dir, "self-cgroup"), filepath.Join(dir, "fs")); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}
