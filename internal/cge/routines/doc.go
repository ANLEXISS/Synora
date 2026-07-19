// Package routines models descriptive contextual routines in memory.
//
// It is intentionally detached from the durable CGE, journal, replay,
// hypotheses and ShadowEngine packages. A caller explicitly supplies chain
// snapshots and applies the resulting occurrences; this package never learns
// automatically and never interprets normality or anomaly.
package routines
