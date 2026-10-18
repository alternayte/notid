package platform

import (
	"sync"
	"time"
)

// Buckets is a token bucket per key: a peer domain, a client address or an actor.
type Buckets struct {
	perMinute float64
	burst     float64

	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

func NewBuckets(perMinute, burst int) *Buckets {
	return &Buckets{perMinute: float64(perMinute), burst: float64(burst), buckets: map[string]*bucket{}}
}

// Allow takes one token for key and reports whether one was left.
func (l *Buckets) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 10_000 {
			l.sweep(now)
		}
		b = &bucket{tokens: l.burst, at: now}
		l.buckets[key] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.at).Minutes()*l.perMinute)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have refilled; they hold no state worth keeping.
func (l *Buckets) sweep(now time.Time) {
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.at).Minutes()*l.perMinute >= l.burst {
			delete(l.buckets, k)
		}
	}
}
