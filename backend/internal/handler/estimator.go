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

package handler

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	corev1 "k8s.io/api/core/v1"

	"github.com/raids-lab/crater/internal/resputil"
	"github.com/raids-lab/crater/internal/util"
	"github.com/raids-lab/crater/pkg/estimator"
)

//nolint:gochecknoinits // This is the standard way to register a gin handler.
func init() {
	Registers = append(Registers, NewEstimatorMgr)
}

// EstimatorMgr 预估器管理
type EstimatorMgr struct {
	name      string
	estimator *estimator.WaitTimeEstimator
}

// NewEstimatorMgr 创建新的预估器管理器
func NewEstimatorMgr(conf *RegisterConfig) Manager {
	return &EstimatorMgr{
		name:      "estimate",
		estimator: estimator.NewWaitTimeEstimator(conf.KubeClient),
	}
}

func (mgr *EstimatorMgr) GetName() string { return mgr.name }

func (mgr *EstimatorMgr) RegisterPublic(_ *gin.RouterGroup) {}

func (mgr *EstimatorMgr) RegisterProtected(g *gin.RouterGroup) {
	g.POST("", mgr.HandleEstimateWaitTime)
}

func (mgr *EstimatorMgr) RegisterAdmin(_ *gin.RouterGroup) {}

type (
	// EstimateItem 单个预估项
	EstimateItem struct {
		Resources corev1.ResourceList              `json:"resources" binding:"required"`
		Selectors []corev1.NodeSelectorRequirement `json:"selectors,omitempty"`
	}

	// EstimateResult 单个预估结果
	EstimateResult struct {
		CanRunImmediately bool          `json:"canRunImmediately"`
		EstimatedWaitTime time.Duration `json:"estimatedWaitTime"`
	}

	// EstimateWaitTimeReq 预估等待时间请求
	EstimateWaitTimeReq struct {
		Requests []EstimateItem `json:"requests" binding:"required"`
	}

	// EstimateWaitTimeResp 预估等待时间响应
	EstimateWaitTimeResp struct {
		Results []EstimateResult `json:"results"`
	}
)

// HandleEstimateWaitTime godoc
//
//	@Summary		批量预估作业等待时间
//	@Description	批量预估多个作业的等待时间
//	@Tags			Estimator
//	@Accept			json
//	@Produce		json
//	@Security		Bearer
//	@Param			request	body		EstimateWaitTimeReq				true	"批量预估请求"
//	@Success		200		{object}	resputil.Response[EstimateWaitTimeResp]	"成功"
//	@Failure		400		{object}	resputil.Response[any]			"请求参数错误"
//	@Failure		500		{object}	resputil.Response[any]			"其他错误"
//	@Router			/v1/estimate [post]
func (mgr *EstimatorMgr) HandleEstimateWaitTime(c *gin.Context) {
	var req EstimateWaitTimeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		resputil.BadRequestError(c, fmt.Sprintf("failed to bind request: %v", err))
		return
	}

	token := util.GetToken(c)

	estimateReqs := make([]*estimator.EstimateRequest, len(req.Requests))
	for i, r := range req.Requests {
		estimateReqs[i] = &estimator.EstimateRequest{
			AccountID: token.AccountID,
			UserID:    token.UserID,
			Resources: r.Resources,
			Selectors: r.Selectors,
		}
	}

	results, err := mgr.estimator.Estimate(c, estimateReqs)
	if err != nil {
		resputil.Error(c, fmt.Sprintf("failed to estimate: %v", err), resputil.NotSpecified)
		return
	}

	resp := EstimateWaitTimeResp{
		Results: make([]EstimateResult, len(results)),
	}
	for i, result := range results {
		resp.Results[i] = EstimateResult{
			CanRunImmediately: result.CanRunImmediately,
			EstimatedWaitTime: result.EstimatedWaitTime,
		}
	}

	resputil.Success(c, resp)
}
