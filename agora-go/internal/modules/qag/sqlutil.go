package qag

import (
	"crypto/rand"
	"errors"
	"strconv"
	"strings"
	"time"

	"agora/internal/javacompat"
	"agora/internal/store"
)

// randomUUID is UUID.randomUUID() (also what Hibernate's GenerationType.UUID
// produces): a random version 4 UUID in canonical lowercase form.
func randomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return javacompat.UUIDFromBytes(b).String()
}

// inList renders Hibernate's expansion of `IN :param` for n values starting at $first.
func inList(first, n int) string {
	var b strings.Builder
	b.WriteByte('(')
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('$')
		b.WriteString(strconv.Itoa(first + i))
	}
	b.WriteByte(')')
	return b.String()
}

// uuidsOrNull is `ids.mapNotNull { it.toUuidOrNull() }` (canonical strings).
func uuidsOrNull(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if u, ok := javacompat.ToUUIDOrNull(id); ok {
			out = append(out, u)
		}
	}
	return out
}

func toArgs(ids []string) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}

// errNullColumn is the NullPointerException / PropertyAccessException Kotlin
// throws when a column mapped to a non-null property (or a primitive) is NULL.
func errNullColumn(table, column string) error {
	return errors.New("java.lang.NullPointerException: " + table + "." + column + " is NULL")
}

// nowMillis is `Date()` / `Calendar.getInstance().time`: the clock at
// millisecond precision, written to timestamp columns.
func nowMillis(now func() time.Time) time.Time { return store.Millis(now()) }
