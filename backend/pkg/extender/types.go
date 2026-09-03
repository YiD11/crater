package extender

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

// Verbs the volcano configmap must point at. onSessionClose is optional: without it the cluster view
// falls back to a time-based rebuild, which behaves the same, only a little more stale.
const (
	JobEnqueueableVerb = "jobEnqueueable"
	OnSessionCloseVerb = "onSessionClose"
)

// Vote values mirror volcano's util.Reject/Abstain. Permit is deliberately never returned: it marks
// the whole tier as decided and would skip capacity's queue capacity check for the same job.
const (
	voteReject  = -1
	voteAbstain = 0
)

const vcJobKind = "Job"

// jobEnqueueableRequest mirrors volcano's payload. api.JobInfo carries no json tags, so its Go field
// names are the wire names; only the fields crater reads are declared here.
type jobEnqueueableRequest struct {
	Job *requestJobInfo `json:"job"`
}

type requestJobInfo struct {
	Name     string           `json:"Name"`
	PodGroup *requestPodGroup `json:"PodGroup"`
}

type requestPodGroup struct {
	Name            string                  `json:"name"`
	OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
}

type jobEnqueueableResponse struct {
	Status int `json:"status"`
}

// ownerJobName resolves the vcjob behind the pod group. Pod groups created by volcano's
// podgroup-controller for plain pods have no vcjob owner and are left alone.
func (req *requestJobInfo) ownerJobName() string {
	if req == nil || req.PodGroup == nil {
		return ""
	}
	for i := range req.PodGroup.OwnerReferences {
		ref := &req.PodGroup.OwnerReferences[i]
		if ref.Controller != nil && *ref.Controller &&
			ref.Kind == vcJobKind && ref.APIVersion == batch.SchemeGroupVersion.String() {
			return ref.Name
		}
	}
	return ""
}

func (req *requestJobInfo) podGroupName() string {
	if req == nil {
		return ""
	}
	if req.PodGroup != nil && req.PodGroup.Name != "" {
		return req.PodGroup.Name
	}
	return req.Name
}
