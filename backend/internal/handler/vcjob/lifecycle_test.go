// Copyright 2026 The Crater Project Team, RAIDS-Lab
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package vcjob

import (
	"context"
	"errors"
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	v1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"github.com/raids-lab/crater/internal/bizerr"
	"github.com/raids-lab/crater/internal/service"
	"github.com/raids-lab/crater/internal/util"
)

const quotaFallbackMessage = "requested resources exceed user queue quota"

func twoReplicaJob() *batch.Job {
	return &batch.Job{Spec: batch.JobSpec{
		Queue: "default",
		Tasks: []batch.TaskSpec{{
			Replicas: 2,
			Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Containers: []v1.Container{{
				Name:      "main",
				Resources: v1.ResourceRequirements{Requests: v1.ResourceList{v1.ResourceCPU: apiresource.MustParse("1")}},
			}}}},
		}},
	}}
}

func TestFormatExceededResourceLimitDetails(t *testing.T) {
	t.Run("lists exceeded resources or falls back", func(t *testing.T) {
		PatchConvey("lists exceeded resources or falls back", t, func() {
			So(formatExceededResourceLimitDetails(nil), ShouldEqual, quotaFallbackMessage)
			within := []service.ResourceLimitDetail{{Resource: "cpu", Used: "1", Limit: "2"}}
			So(formatExceededResourceLimitDetails(within), ShouldEqual, quotaFallbackMessage)

			details := []service.ResourceLimitDetail{
				{Resource: "cpu", Used: "3", Limit: "2", Exceeded: true},
				{Resource: "memory", Used: "1Gi", Limit: "2Gi"},
				{Resource: "nvidia.com/a100", Used: "2", Limit: "1", Exceeded: true},
			}
			So(formatExceededResourceLimitDetails(details), ShouldEqual,
				"cpu requested 3 exceeds limit 2; nvidia.com/a100 requested 2 exceeds limit 1")
		})
	})
}

func TestCheckRequestedResourceLimit(t *testing.T) {
	token := util.JWTMessage{UserID: 7, AccountID: 1}

	t.Run("without a quota service every request passes", func(t *testing.T) {
		PatchConvey("without a quota service every request passes", t, func() {
			So((&VolcanojobMgr{}).checkRequestedResourceLimit(t.Context(), token, twoReplicaJob()), ShouldBeNil)
		})
	})

	t.Run("hands the replicated demand to the service", func(t *testing.T) {
		PatchConvey("hands the replicated demand to the service", t, func() {
			var seenQueue string
			var seenRequest map[string]string
			Mock((*service.QueueQuotaService).CheckRequestedResourceLimit).To(
				func(_ *service.QueueQuotaService, _ context.Context, _, _ uint, queue string, requested map[string]string,
				) (*service.ResourceLimitCheckResult, error) {
					seenQueue = queue
					seenRequest = requested
					return &service.ResourceLimitCheckResult{Enabled: true}, nil
				}).Build()

			mgr := &VolcanojobMgr{queueQuotaSvc: &service.QueueQuotaService{}}
			So(mgr.checkRequestedResourceLimit(t.Context(), token, twoReplicaJob()), ShouldBeNil)
			So(seenQueue, ShouldEqual, "default")
			So(seenRequest, ShouldResemble, map[string]string{"cpu": "2"})
		})
	})

	t.Run("service failures and exceeded requests", func(t *testing.T) {
		PatchConvey("service failure", t, func() {
			Mock((*service.QueueQuotaService).CheckRequestedResourceLimit).
				Return(nil, bizerr.Internal.DatabaseError.New("db down")).Build()
			mgr := &VolcanojobMgr{queueQuotaSvc: &service.QueueQuotaService{}}
			err := mgr.checkRequestedResourceLimit(t.Context(), token, twoReplicaJob())
			So(errors.Is(err, bizerr.Internal.Base), ShouldBeTrue)
		})
		PatchConvey("exceeded request", t, func() {
			Mock((*service.QueueQuotaService).CheckRequestedResourceLimit).Return(&service.ResourceLimitCheckResult{
				Enabled:  true,
				Exceeded: true,
				Details:  []service.ResourceLimitDetail{{Resource: "cpu", Used: "3", Limit: "2", Exceeded: true}},
			}, nil).Build()
			mgr := &VolcanojobMgr{queueQuotaSvc: &service.QueueQuotaService{}}
			err := mgr.checkRequestedResourceLimit(t.Context(), token, twoReplicaJob())
			So(errors.Is(err, bizerr.BadRequest.Base), ShouldBeTrue)
			So(err.Error(), ShouldContainSubstring, "cpu requested 3 exceeds limit 2")
		})
	})
}
