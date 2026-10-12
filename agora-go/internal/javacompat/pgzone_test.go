package javacompat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPostgresTimeZone(t *testing.T) {
	for in, want := range map[string]string{
		"Europe/Paris": "Europe/Paris", "UTC": "UTC", "GMT": "GMT", "GMT+02:00": "GMT-02:00",
		"GMT-05:30": "GMT+05:30", "GMTX": "GMTX", "Etc/GMT+2": "Etc/GMT+2",
	} {
		if got := PostgresTimeZone(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestJVMDefaultZoneID(t *testing.T) {
	dir := t.TempDir()
	savedTZ, savedLT := etcTimezone, etcLocaltime
	t.Cleanup(func() { etcTimezone, etcLocaltime = savedTZ, savedLT })
	etcTimezone, etcLocaltime = filepath.Join(dir, "timezone"), filepath.Join(dir, "localtime")
	env := func(tz string) func(string) string {
		return func(k string) string {
			if k == "TZ" {
				return tz
			}
			return ""
		}
	}
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	if got := JVMDefaultZoneID(env("Europe/Paris"), now); got != "Europe/Paris" {
		t.Errorf("TZ: %q", got)
	}
	if got := JVMDefaultZoneID(env(":America/Santiago"), now); got != "America/Santiago" {
		t.Errorf("TZ with ':': %q", got)
	}
	// no /etc files: UTC
	if got := JVMDefaultZoneID(env(""), now); got != "UTC" {
		t.Errorf("nothing: %q", got)
	}
	if err := os.Symlink("/usr/share/zoneinfo/Asia/Tokyo", etcLocaltime); err != nil {
		t.Fatal(err)
	}
	if got := JVMDefaultZoneID(env(""), now); got != "Asia/Tokyo" {
		t.Errorf("/etc/localtime: %q", got)
	}
	if err := os.WriteFile(etcTimezone, []byte("Europe/Berlin\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := JVMDefaultZoneID(env(""), now); got != "Europe/Berlin" {
		t.Errorf("/etc/timezone first: %q", got)
	}
}
