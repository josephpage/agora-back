package javacompat

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// Paths read like the JDK's TimeZone_md.c (replaced in tests).
var (
	etcTimezone  = "/etc/timezone"
	etcLocaltime = "/etc/localtime"
)

// JVMDefaultZoneID is TimeZone.getDefault().getID() of a JVM started in this environment on
// Linux without -Duser.timezone: TZ (a leading ':' dropped) when it is set and not empty, else the
// zone named by /etc/timezone, else the zone /etc/localtime links to, else "UTC". A TZ the zone
// database does not know becomes the fixed offset in force at now, "GMT±hh:mm" ("GMT" when it is
// zero), like the JDK's fallback on the C library.
func JVMDefaultZoneID(getenv func(string) string, now time.Time) string {
	if tz := strings.TrimPrefix(getenv("TZ"), ":"); tz != "" {
		if _, err := time.LoadLocation(tz); err == nil && !strings.HasPrefix(tz, "/") {
			return tz
		}
		_, off := now.In(time.Local).Zone()
		return gmtOffsetID(off)
	}
	if b, err := os.ReadFile(etcTimezone); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id
		}
	}
	if target, err := os.Readlink(etcLocaltime); err == nil {
		if i := strings.Index(target, "zoneinfo/"); i >= 0 {
			return target[i+len("zoneinfo/"):]
		}
	}
	return "UTC"
}

func gmtOffsetID(seconds int) string {
	if seconds == 0 {
		return "GMT"
	}
	sign := '+'
	if seconds < 0 {
		sign, seconds = '-', -seconds
	}
	return fmt.Sprintf("GMT%c%02d:%02d", sign, seconds/3600, seconds/60%60)
}

// PostgresTimeZone is pgjdbc's createPostgresTimeZone: the TimeZone startup parameter it sends for
// the JVM zone id (a custom "GMT+hh:mm" id has its sign inverted, the POSIX convention of
// PostgreSQL). The session zone decides how the SQL compares timestamp columns with
// CURRENT_TIMESTAMP.
func PostgresTimeZone(id string) string {
	if len(id) <= 3 || !strings.HasPrefix(id, "GMT") {
		return id
	}
	switch id[3] {
	case '+':
		return "GMT-" + id[4:]
	case '-':
		return "GMT+" + id[4:]
	}
	return id
}
