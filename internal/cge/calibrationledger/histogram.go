package calibrationledger

const permilleBuckets = 1001

type PermilleHistogram struct {
	buckets [permilleBuckets]uint64
	count   uint64
	sum     uint64
}

func (h *PermilleHistogram) Add(v int) {
	if v < 0 {
		v = 0
	}
	if v > 1000 {
		v = 1000
	}
	h.buckets[v]++
	h.count++
	h.sum += uint64(v)
}
func (h PermilleHistogram) Mean() int {
	if h.count == 0 {
		return 0
	}
	return int((h.sum + h.count/2) / h.count)
}
func (h PermilleHistogram) Percentile(percentile int) int {
	if h.count == 0 {
		return 0
	}
	if percentile < 0 {
		percentile = 0
	}
	if percentile > 1000 {
		percentile = 1000
	}
	rank := (h.count*uint64(percentile) + 999) / 1000
	if rank == 0 {
		rank = 1
	}
	var seen uint64
	for i, n := range h.buckets {
		seen += n
		if seen >= rank {
			return i
		}
	}
	return 1000
}
