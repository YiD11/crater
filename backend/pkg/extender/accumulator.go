package extender

import (
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/pkg/utils"
)

// reservationTTL drains entries whose job never reached Inqueue, e.g. an earlier-tier plugin started
// rejecting it so the extender is no longer asked and no admission event will ever arrive.
const reservationTTL = 30 * time.Second

type reservation struct {
	userID    uint
	queue     string
	resources v1.ResourceList
	at        time.Time
}

// sessionAccumulator tracks jobs this process已放行 but whose admission volcano writes back only at
// session close. Without it a batch submission is measured against one stale usage snapshot and the
// whole batch passes the same quota check.
type sessionAccumulator struct {
	mu      sync.Mutex
	entries map[string]*reservation
	now     func() time.Time
}

func newSessionAccumulator() *sessionAccumulator {
	return &sessionAccumulator{
		entries: make(map[string]*reservation),
		now:     utils.GetLocalTime,
	}
}

func (a *sessionAccumulator) reserve(view *jobView) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries[view.name] = &reservation{
		userID:    view.userID,
		queue:     view.queue,
		resources: view.resources,
		at:        a.now(),
	}
}

// sweep releases a reservation as soon as the pod group cache confirms the admission it stood in for,
// which is the same condition that makes the job show up in the admitted usage sum.
func (a *sessionAccumulator) sweep(snap *snapshot) {
	deadline := a.now().Add(-reservationTTL)
	a.mu.Lock()
	defer a.mu.Unlock()
	for name, entry := range a.entries {
		view, ok := snap.byName[name]
		if !ok || view.admitted || view.terminal || entry.at.Before(deadline) {
			delete(a.entries, name)
		}
	}
}

// reservedExcluding skips the inspected job's own standing reservation, which would otherwise be
// counted twice against it when volcano asks about the same job in a later session.
func (a *sessionAccumulator) reservedExcluding(userID uint, queue, jobName string) v1.ResourceList {
	a.mu.Lock()
	defer a.mu.Unlock()
	total := v1.ResourceList{}
	for name, entry := range a.entries {
		if name == jobName || entry.userID != userID || entry.queue != queue {
			continue
		}
		total = utils.SumResources(total, entry.resources)
	}
	return total
}
