/*
Copyright 2025 RAIDS Lab

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package estimator 提供作业等待时间预估功能
package estimator

import (
	"context"
	"fmt"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"github.com/raids-lab/crater/pkg/crclient"
	"github.com/raids-lab/crater/pkg/utils"
)

// WaitTimeEstimator 等待时间预估器
type WaitTimeEstimator struct {
	kubeClient kubernetes.Interface
}

// NewWaitTimeEstimator 创建新的预估器
func NewWaitTimeEstimator(kubeClient kubernetes.Interface) *WaitTimeEstimator {
	return &WaitTimeEstimator{
		kubeClient: kubeClient,
	}
}

// Estimate 预估作业等待时间
func (e *WaitTimeEstimator) Estimate(ctx context.Context, reqs []*EstimateRequest) ([]*EstimateResponse, error) {
	results := make([]*EstimateResponse, len(reqs))
	nodes, err := e.kubeClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		err := fmt.Errorf("WaitTimeEstimator.Estimate: %w", err)
		klog.Error(err)
		return nil, err
	}
	for i, req := range reqs {
		filteredNodes := crclient.FilterNodesBySelectors(nodes.Items, req.Selectors)
		states := lo.FilterMap(filteredNodes, func(node *corev1.Node, _ int) (NodeResourceState, bool) {
			if !crclient.IsNodeSchedulable(node) {
				return NodeResourceState{}, false
			}

			state, err := e.buildNodeResourceState(ctx, node)
			if err != nil {
				return NodeResourceState{}, false
			}
			return state, true
		})
		if len(states) == 0 {
			results[i] = &EstimateResponse{
				CanRunImmediately: false,
				EstimatedWaitTime: -1,
			}
			continue
		}
		waitTime := calculateWaitTime(states, req.Resources)
		results[i] = &EstimateResponse{
			CanRunImmediately: waitTime == 0,
			EstimatedWaitTime: waitTime,
		}
	}
	return results, nil
}

// buildNodeResourceState 构建节点资源状态
func (e *WaitTimeEstimator) buildNodeResourceState(ctx context.Context, node *corev1.Node) (NodeResourceState, error) {
	totalResource := node.Status.Allocatable.DeepCopy()

	pods, err := e.kubeClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node.Name + ",status.phase=Running",
	})
	if err != nil {
		return NodeResourceState{}, err
	}

	usedResource := make(corev1.ResourceList)
	var runningJobs []RunningJobInfo

	for i := range pods.Items {
		pod := &pods.Items[i]

		podResources := utils.CalculateRequsetsByContainers(pod.Spec.Containers)
		usedResource = utils.SumResources(usedResource, podResources)

		if jobInfo := extractJobInfo(pod); jobInfo != nil {
			runningJobs = append(runningJobs, *jobInfo)
		}
	}

	freeResource := utils.SubtractResourceList(totalResource, usedResource)

	return NodeResourceState{
		NodeName:      node.Name,
		TotalResource: totalResource,
		UsedResource:  usedResource,
		FreeResource:  freeResource,
		RunningJobs:   runningJobs,
	}, nil
}
