package format

import (
	"fmt"
	"time"
)

func Timestamp(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format(time.RFC3339)
}

func KeyValue(key, value string) string {
	return fmt.Sprintf("%s: %s", key, value)
}
