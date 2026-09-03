package extender

import (
	"context"
	"strconv"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"github.com/raids-lab/crater/internal/service"
	vcjobservice "github.com/raids-lab/crater/internal/service/vcjob"
	"github.com/raids-lab/crater/pkg/config"
	"github.com/raids-lab/crater/pkg/utils"
)

// jobView joins the two caches the decision needs: the vcjob supplies demand and constraints, the
// pod group supplies the admission phase. The pair is strictly 1:1.
type jobView struct {
	name      string
	userID    uint
	queue     string
	resources v1.ResourceList
	minimum   v1.ResourceList
	domain    string
	nodes     sets.Set[string]
	admitted  bool
	terminal  bool
	waiting   bool
	timedOut  bool
}

// occupiesQuota follows the design rule: only admitted, unfinished jobs consume a user's limit, so a
// job held back by its own quota never counts against itself.
func (v *jobView) occupiesQuota() bool {
	return v.admitted && !v.terminal
}

// ownerKey groups the quota ledger by user and queue; the account dimension is implied because a
// queue never spans accounts.
type ownerKey struct {
	userID uint
	queue  string
}

type snapshot struct {
	views        []*jobView
	byName       map[string]*jobView
	queues       map[string]*scheduling.Queue
	quotas       *service.QueueQuotaSet
	usageByOwner map[ownerKey]v1.ResourceList
}

func (s *Server) buildSnapshot(ctx context.Context, current *settings) (*snapshot, error) {
	namespace := config.GetConfig().Namespaces.Job
	// Deep copies are skipped because nothing below mutates the cached objects.
	var jobs batch.JobList
	if listErr := s.reader.List(ctx, &jobs, client.InNamespace(namespace), client.UnsafeDisableDeepCopy); listErr != nil {
		return nil, listErr
	}
	var groups scheduling.PodGroupList
	if listErr := s.reader.List(ctx, &groups, client.InNamespace(namespace), client.UnsafeDisableDeepCopy); listErr != nil {
		return nil, listErr
	}
	var queues scheduling.QueueList
	if listErr := s.reader.List(ctx, &queues, client.UnsafeDisableDeepCopy); listErr != nil {
		return nil, listErr
	}

	groupsByJob := podGroupsByOwnerJob(&groups)
	now := utils.GetLocalTime()
	snap := &snapshot{
		views:        make([]*jobView, 0, len(jobs.Items)),
		byName:       make(map[string]*jobView, len(jobs.Items)),
		queues:       make(map[string]*scheduling.Queue, len(queues.Items)),
		quotas:       current.quotas,
		usageByOwner: make(map[ownerKey]v1.ResourceList),
	}
	for i := range queues.Items {
		snap.queues[queues.Items[i].Name] = &queues.Items[i]
	}
	for i := range jobs.Items {
		job := &jobs.Items[i]
		view := newJobView(job, groupsByJob[job.Name], now)
		snap.views = append(snap.views, view)
		snap.byName[view.name] = view
		if view.occupiesQuota() {
			key := ownerKey{userID: view.userID, queue: view.queue}
			snap.usageByOwner[key] = utils.SumResources(snap.usageByOwner[key], view.resources)
		}
	}
	return snap, nil
}

func podGroupsByOwnerJob(groups *scheduling.PodGroupList) map[string]*scheduling.PodGroup {
	result := make(map[string]*scheduling.PodGroup, len(groups.Items))
	for i := range groups.Items {
		group := &groups.Items[i]
		if owner := controllerOwnerJobName(group.OwnerReferences); owner != "" {
			result[owner] = group
		}
	}
	return result
}

func controllerOwnerJobName(refs []metav1.OwnerReference) string {
	for i := range refs {
		ref := &refs[i]
		if ref.Controller != nil && *ref.Controller &&
			ref.Kind == vcJobKind && ref.APIVersion == batch.SchemeGroupVersion.String() {
			return ref.Name
		}
	}
	return ""
}

func newJobView(job *batch.Job, group *scheduling.PodGroup, now time.Time) *jobView {
	phase := job.Status.State.Phase
	resources := vcjobservice.CalculateJobResources(job)
	view := &jobView{
		name:      job.Name,
		userID:    annotationUserID(job.Annotations),
		queue:     job.Spec.Queue,
		resources: resources,
		minimum:   resources,
		domain:    utils.GetResourceDomain(resources),
		nodes:     utils.GetJobExplicitNodeNames(job),
		admitted:  isAdmitted(group),
		terminal:  isTerminalPhase(phase),
	}
	if group != nil && group.Spec.MinResources != nil {
		view.minimum = *group.Spec.MinResources
	}
	view.waiting = !view.terminal && (phase == "" || phase == batch.Pending)
	if tolerance := annotationTolerance(job.Annotations); tolerance != nil && view.waiting {
		view.timedOut = now.After(job.CreationTimestamp.Add(*tolerance))
	}
	return view
}

func isAdmitted(group *scheduling.PodGroup) bool {
	if group == nil {
		return false
	}
	return group.Status.Phase != "" && group.Status.Phase != scheduling.PodGroupPending
}

func isTerminalPhase(phase batch.JobPhase) bool {
	switch phase {
	case batch.Completed, batch.Failed, batch.Aborted, batch.Terminated:
		return true
	default:
		return false
	}
}

func annotationUserID(annotations map[string]string) uint {
	userID, err := strconv.ParseUint(annotations[vcjobservice.AnnotationKeyUserID], 10, 64)
	if err != nil {
		return 0
	}
	return uint(userID)
}

// annotationTolerance returns nil for a job that carries no tolerance, which then never times out and
// never blocks anyone. Crater stamps the annotation at submission, so this only covers jobs created
// outside the platform.
func annotationTolerance(annotations map[string]string) *time.Duration {
	seconds, err := strconv.ParseInt(annotations[vcjobservice.AnnotationKeyWaitingToleranceSeconds], 10, 64)
	if err != nil || seconds <= 0 {
		return nil
	}
	tolerance := time.Duration(seconds) * time.Second
	return &tolerance
}
