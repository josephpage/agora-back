package users

import (
	"crypto/rand"
	"time"

	"agora/internal/javacompat"
	"agora/internal/store"
)

// RandomUUID is UUID.randomUUID() (also what Hibernate's GenerationType.UUID
// produces): a random version 4 UUID in canonical lowercase form.
func RandomUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return javacompat.UUIDFromBytes(b).String()
}

// storeMillis is the value of a `java.util.Date()` written to a timestamp column.
func storeMillis(t time.Time) time.Time { return store.Millis(t) }
