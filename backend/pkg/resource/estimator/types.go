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

package estimator

import (
	"time"

	corev1 "k8s.io/api/core/v1"
)

// EstimateRequest 预估请求
type EstimateRequest struct {
	AccountID uint                             `json:"accountId"`
	UserID    uint                             `json:"userId"`
	Resources corev1.ResourceList              `json:"resources"`
	Selectors []corev1.NodeSelectorRequirement `json:"selectors,omitempty"`
}

// EstimateResponse 预估响应
type EstimateResponse struct {
	CanRunImmediately bool          `json:"canRunImmediately"`
	EstimatedWaitTime time.Duration `json:"estimatedWaitTime"`
}

// NodeResourceState 节点资源状态
type NodeResourceState struct {
	NodeName      string
	TotalResource corev1.ResourceList
	UsedResource  corev1.ResourceList
	FreeResource  corev1.ResourceList
	RunningJobs   []RunningJobInfo
}

// RunningJobInfo 运行中作业信息
type RunningJobInfo struct {
	JobName    string
	Resources  corev1.ResourceList
	StartTime  time.Time
	MaxRunTime *time.Duration // 最大运行时间（如果设置）
}

// QueuedJobInfo 排队中作业信息
type QueuedJobInfo struct {
	JobName   string
	AccountID uint
	UserID    uint
	Resources corev1.ResourceList
	QueuedAt  time.Time
	Priority  int
}
