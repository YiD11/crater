package estimator

import (
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/pkg/constants"
	"github.com/raids-lab/crater/pkg/utils"
)

// calculateWaitTime 计算等待时间
func calculateWaitTime(
	nodeStates []NodeResourceState,
	requestedResources corev1.ResourceList,
) time.Duration {
	for _, nodeState := range nodeStates {
		if utils.CanFitResources(nodeState.FreeResource, requestedResources) {
			return 0
		}
	}

	minWaitTime := time.Duration(-1)

	for _, nodeState := range nodeStates {
		if !utils.CanFitResources(nodeState.TotalResource, requestedResources) {
			continue
		}

		waitTime := calculateNodeWaitTime(nodeState, requestedResources)
		if waitTime >= 0 && (minWaitTime < 0 || waitTime < minWaitTime) {
			minWaitTime = waitTime
		}
	}

	return minWaitTime
}

// calculateNodeWaitTime 计算在特定节点上需要等待的时间
func calculateNodeWaitTime(
	nodeState NodeResourceState,
	requestedResources corev1.ResourceList,
) time.Duration {
	if len(nodeState.RunningJobs) == 0 {
		return -1
	}

	sortedJobs := make([]RunningJobInfo, len(nodeState.RunningJobs))
	copy(sortedJobs, nodeState.RunningJobs)
	nowTime := time.Now()
	sort.Slice(sortedJobs, func(i, j int) bool {
		si := sortedJobs[i].StartTime
		sj := sortedJobs[j].StartTime
		ri := sortedJobs[i].MaxRunTime
		rj := sortedJobs[j].MaxRunTime
		if ri == nil && rj == nil {
			return sortedJobs[i].StartTime.Before(sortedJobs[j].StartTime)
		}
		if ri == nil {
			return false
		}
		if rj == nil {
			return true
		}
		wi := si.Add(*ri).Sub(nowTime)
		wj := sj.Add(*rj).Sub(nowTime)
		return wi < wj
	})

	simulatedFree := nodeState.FreeResource.DeepCopy()
	var maxWaitTime time.Duration
	for _, job := range sortedJobs {
		if job.MaxRunTime == nil {
			continue
		}

		simulatedFree = utils.SumResources(simulatedFree, job.Resources)
		waitTime := job.StartTime.Add(*job.MaxRunTime).Sub(nowTime)
		maxWaitTime = max(maxWaitTime, waitTime)

		if utils.CanFitResources(simulatedFree, requestedResources) {
			return maxWaitTime
		}
	}

	return -1
}

// extractJobInfo 从 Pod 提取作业信息
func extractJobInfo(pod *corev1.Pod) *RunningJobInfo {
	for _, owner := range pod.OwnerReferences {
		if owner.Kind != "Job" || !strings.Contains(owner.APIVersion, "volcano") {
			continue
		}
		startTime := pod.Status.StartTime
		if startTime == nil {
			continue
		}

		var maxRunTime *time.Duration
		if val, ok := pod.Annotations[constants.AnnotationKeyMaxRunTime]; ok {
			if t, err := time.ParseDuration(val); err == nil {
				maxRunTime = &t
			}
		}

		return &RunningJobInfo{
			JobName:    owner.Name,
			Resources:  utils.CalculateRequsetsByContainers(pod.Spec.Containers),
			StartTime:  startTime.Time,
			MaxRunTime: maxRunTime,
		}
	}
	return nil
}
