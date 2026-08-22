// Package retention contains the shared, deterministic V1 data-lifecycle
// policy. Product stores remain owners of their records; this package only
// defines limits, ordering and the filesystem reserve check they apply.
package retention

import (
	"errors"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"
)

type Category string

const (
	CategoryClips     Category = "clips"
	CategoryIncidents Category = "incidents"
	CategoryEvents    Category = "events"
	CategoryLogs      Category = "logs"
	CategoryOutbox    Category = "outbox"
	CategoryTemporary Category = "temporary"
)

type Limit struct {
	MaxAge   time.Duration
	MaxCount int
	MaxBytes int64
}

type Policy struct {
	Clips        Limit
	Incidents    Limit
	Events       Limit
	Logs         Limit
	Outbox       Limit
	Temporary    Limit
	MinFreeBytes int64
}

func DefaultPolicy() Policy {
	return Policy{
		Clips:        Limit{MaxAge: 24 * time.Hour, MaxCount: 500, MaxBytes: 5 << 30},
		Incidents:    Limit{MaxAge: 90 * 24 * time.Hour, MaxCount: 200},
		Events:       Limit{MaxAge: 7 * 24 * time.Hour, MaxCount: 200},
		Logs:         Limit{MaxAge: 14 * 24 * time.Hour, MaxCount: 10000, MaxBytes: 512 << 20},
		Outbox:       Limit{MaxAge: 7 * 24 * time.Hour, MaxCount: 10000, MaxBytes: 256 << 20},
		Temporary:    Limit{MaxAge: time.Hour, MaxCount: 4096, MaxBytes: 512 << 20},
		MinFreeBytes: 512 << 20,
	}
}

func (p Policy) Validate() error {
	for _, limit := range []Limit{p.Clips, p.Incidents, p.Events, p.Logs, p.Outbox, p.Temporary} {
		if limit.MaxAge <= 0 || limit.MaxCount <= 0 || limit.MaxBytes < 0 {
			return errors.New("retention limits must be positive")
		}
	}
	if p.MinFreeBytes <= 0 {
		return errors.New("retention minimum free space must be positive")
	}
	return nil
}

type Entry struct {
	ID        string
	Category  Category
	CreatedAt time.Time
	UpdatedAt time.Time
	SizeBytes int64
	Protected bool
}

// SelectExpired returns unprotected records in deterministic deletion order.
// Age is evaluated first; count and byte quotas then select the oldest
// remaining records. Equal timestamps are ordered by stable ID.
func (p Policy) SelectExpired(entries []Entry, now time.Time) []Entry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	byCategory := make(map[Category][]Entry)
	for _, entry := range entries {
		if strings.TrimSpace(entry.ID) == "" || entry.Protected || entry.SizeBytes < 0 {
			continue
		}
		byCategory[entry.Category] = append(byCategory[entry.Category], entry)
	}
	result := make([]Entry, 0)
	for category, values := range byCategory {
		limit, ok := p.limit(category)
		if !ok {
			continue
		}
		sort.SliceStable(values, func(i, j int) bool {
			left, right := retentionTime(values[i]), retentionTime(values[j])
			if left.Equal(right) {
				return values[i].ID < values[j].ID
			}
			return left.Before(right)
		})
		selected := make(map[string]struct{})
		var totalBytes int64
		for _, entry := range values {
			totalBytes += entry.SizeBytes
		}
		for _, entry := range values {
			if !retentionExpired(entry, now, limit.MaxAge) {
				continue
			}
			selected[entry.ID] = struct{}{}
			result = append(result, entry)
		}
		remainingCount, remainingBytes := len(values)-len(selected), totalBytes
		for _, entry := range values {
			if _, already := selected[entry.ID]; already {
				remainingBytes -= entry.SizeBytes
				continue
			}
			if remainingCount <= limit.MaxCount && (limit.MaxBytes <= 0 || remainingBytes <= limit.MaxBytes) {
				break
			}
			selected[entry.ID] = struct{}{}
			result = append(result, entry)
			remainingCount--
			remainingBytes -= entry.SizeBytes
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		left, right := retentionTime(result[i]), retentionTime(result[j])
		if left.Equal(right) {
			return result[i].ID < result[j].ID
		}
		return left.Before(right)
	})
	return result
}

func (p Policy) limit(category Category) (Limit, bool) {
	switch category {
	case CategoryClips:
		return p.Clips, true
	case CategoryIncidents:
		return p.Incidents, true
	case CategoryEvents:
		return p.Events, true
	case CategoryLogs:
		return p.Logs, true
	case CategoryOutbox:
		return p.Outbox, true
	case CategoryTemporary:
		return p.Temporary, true
	default:
		return Limit{}, false
	}
}

func retentionTime(entry Entry) time.Time {
	if !entry.UpdatedAt.IsZero() {
		return entry.UpdatedAt
	}
	return entry.CreatedAt
}

func retentionExpired(entry Entry, now time.Time, maxAge time.Duration) bool {
	at := retentionTime(entry)
	return !at.IsZero() && now.Sub(at) >= maxAge
}

func FreeBytes(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func HasReserve(path string, incomingBytes, minFreeBytes int64) (bool, error) {
	if incomingBytes < 0 || minFreeBytes <= 0 {
		return false, errors.New("invalid retention reserve")
	}
	if _, err := os.Stat(path); err != nil {
		return false, err
	}
	free, err := FreeBytes(path)
	if err != nil {
		return false, err
	}
	return free-incomingBytes >= minFreeBytes, nil
}
