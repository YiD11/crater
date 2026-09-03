package extender

import (
	"context"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/internal/service"
	"github.com/raids-lab/crater/pkg/utils"
)

// maxQueueHierarchyDepth guards the parent walk against a mis-configured queue cycle.
const maxQueueHierarchyDepth = 8

// vote carries the answer plus the structured log fields that are the only diagnosis channel volcano
// leaves available for extender decisions.
type vote struct {
	status int
	fields []any
}

// decide runs the design's vote order: per-user queue quota first, then timeout blocking. Every exit
// is either Reject or Abstain.
func (s *Server) decide(ctx context.Context, req *requestJobInfo) vote {
	jobName := req.ownerJobName()
	if jobName == "" {
		return vote{voteAbstain, []any{"podGroup", req.podGroupName(), "skip", "not owned by a vcjob"}}
	}

	state, err := s.currentSession(ctx)
	if err != nil {
		return vote{voteAbstain, []any{"job", jobName, "skip", "session state unavailable", "error", err.Error()}}
	}
	if state.snap == nil {
		return vote{status: voteAbstain}
	}
	snap := state.snap

	candidate, ok := snap.byName[jobName]
	if !ok {
		return vote{voteAbstain, []any{"job", jobName, "skip", "vcjob not in cache yet"}}
	}

	if result := snap.quotaVerdict(candidate, s.accumulator); result != nil && result.Exceeded {
		return vote{voteReject, []any{
			"job", jobName,
			"cause", "queue quota exceeded",
			"queue", candidate.queue,
			"userID", candidate.userID,
			"quota", formatQuotaDetails(result.Details),
		}}
	}

	if blocker := snap.blockerFor(candidate, s.accumulator); blocker != nil {
		return vote{voteReject, []any{
			"job", jobName,
			"cause", "blocked by timed out job",
			"queue", candidate.queue,
			"blocker", blocker.name,
			"blockerAdmitted", blocker.admitted,
			"blockerResources", utils.ResourceListSummary(blocker.resources),
		}}
	}

	s.accumulator.reserve(candidate)
	return vote{status: voteAbstain}
}

// quotaVerdict returns nil when no limit applies, including for jobs created outside crater, which
// carry no user annotation and therefore belong to no user's ledger.
func (snap *snapshot) quotaVerdict(candidate *jobView, acc *sessionAccumulator) *service.ResourceLimitCheckResult {
	if candidate.userID == 0 {
		return nil
	}
	resolved := snap.quotas.Resolve(candidate.queue)
	if !resolved.Enabled {
		return nil
	}
	used := utils.SumResources(
		snap.usageByOwner[ownerKey{userID: candidate.userID, queue: candidate.queue}],
		acc.reservedExcluding(candidate.userID, candidate.queue, candidate.name),
	)
	return service.EvaluateQueueQuota(resolved, used, candidate.resources)
}

// blockerFor applies the asymmetry between the two blocker classes: an admitted timed-out job already
// queued ahead of everyone blocks every candidate, while a still unadmitted one lets equally timed-out
// peers through so a whole batch cannot deadlock on itself.
func (snap *snapshot) blockerFor(candidate *jobView, acc *sessionAccumulator) *jobView {
	for _, view := range snap.views {
		if view.name == candidate.name || !view.timedOut || !view.waiting {
			continue
		}
		if !inBlockingScope(view, candidate) {
			continue
		}
		if view.admitted {
			return view
		}
		if candidate.timedOut {
			continue
		}
		if snap.hasBlockingPower(view, acc) {
			return view
		}
	}
	return nil
}

func inBlockingScope(blocker, candidate *jobView) bool {
	return blocker.queue == candidate.queue &&
		utils.CanResourceDomainBlock(blocker.domain, candidate.domain) &&
		utils.NodeConstraintsOverlap(blocker.nodes, candidate.nodes)
}

// hasBlockingPower is computed fresh every round, never remembered. A job waiting on its own user's
// quota would otherwise deadlock the queue on itself, and a job larger than its queue capacity would
// freeze the queue forever because it can never be admitted.
func (snap *snapshot) hasBlockingPower(blocker *jobView, acc *sessionAccumulator) bool {
	result := snap.quotaVerdict(blocker, acc)
	if result != nil && result.Exceeded {
		return false
	}
	return snap.fitsQueueCapability(blocker)
}

// fitsQueueCapability mirrors capacity's hierarchical check: the minimum demand must fit the leaf
// queue and every ancestor.
func (snap *snapshot) fitsQueueCapability(view *jobView) bool {
	name := view.queue
	for depth := 0; name != "" && depth < maxQueueHierarchyDepth; depth++ {
		queue, ok := snap.queues[name]
		if !ok {
			return true
		}
		if !fitsCapability(view.minimum, queue.Spec.Capability) {
			return false
		}
		name = queue.Spec.Parent
	}
	return true
}

// fitsCapability treats resources absent from capability as unlimited, matching volcano's semantics.
func fitsCapability(required, capability v1.ResourceList) bool {
	for name, limit := range capability {
		if requested, ok := required[name]; ok && requested.Cmp(limit) > 0 {
			return false
		}
	}
	return true
}

func formatQuotaDetails(details []service.ResourceLimitDetail) string {
	parts := make([]string, 0, len(details))
	for i := range details {
		detail := &details[i]
		if !detail.Exceeded {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s used %s limit %s", detail.Resource, detail.Used, detail.Limit))
	}
	return strings.Join(parts, "; ")
}
