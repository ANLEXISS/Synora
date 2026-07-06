package idgen

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var counter uint64

func New(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "id"
	}

	now := time.Now().UTC().UnixMilli()
	seq := atomic.AddUint64(&counter, 1)
	return fmt.Sprintf("%s-%d-%d", prefix, now, seq)
}
