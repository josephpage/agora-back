package notification

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// More than 65535 ids: the Kotlin statement is refused by pgjdbc before anything is deleted.
func TestDeleteUsersNotificationsParameterLimit(t *testing.T) {
	ids := make([]string, maxJDBCParameters+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("00000000-0000-4000-9001-%012x", i)
	}
	ids = append(ids, "not-a-uuid")
	r := &Repository{} // no database: the limit is checked first
	if err := r.DeleteUsersNotifications(context.Background(), ids); !errors.Is(err, errJavaException) {
		t.Fatalf("got %v", err)
	}
}
