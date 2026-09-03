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

package service

import (
	"context"
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	"k8s.io/utils/ptr"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/internal/bizerr"
)

func TestGetSchedulerExtenderConfig(t *testing.T) {
	t.Run("parses the stored switches", func(t *testing.T) {
		PatchConvey("parses the stored switches", t, func() {
			var seen []string
			Mock((*ConfigService).getConfigs).To(func(_ *ConfigService, _ context.Context, keys ...string) (map[string]string, error) {
				seen = keys
				return map[string]string{
					model.ConfigKeySchedulerExtenderEnabled:   "true",
					model.ConfigKeyJobWaitingToleranceSeconds: "600",
				}, nil
			}).Build()

			cfg, err := (&ConfigService{}).GetSchedulerExtenderConfig(t.Context())
			So(err, ShouldBeNil)
			So(cfg.SchedulerExtenderEnabled, ShouldBeTrue)
			So(cfg.QueueQuotaEnabled, ShouldBeFalse)
			So(cfg.JobWaitingToleranceSeconds, ShouldEqual, 600)
			So(seen, ShouldResemble, []string{
				model.ConfigKeySchedulerExtenderEnabled,
				model.ConfigKeyQueueQuotaEnabled,
				model.ConfigKeyJobWaitingToleranceSeconds,
			})
		})
	})

	t.Run("query failure", func(t *testing.T) {
		PatchConvey("query failure", t, func() {
			Mock((*ConfigService).getConfigs).Return(nil, bizerr.Internal.DatabaseError.New("db down")).Build()
			cfg, err := (&ConfigService{}).GetSchedulerExtenderConfig(t.Context())
			So(cfg, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestUpdateSchedulerExtenderConfig(t *testing.T) {
	svc := &ConfigService{}

	t.Run("validates before touching the table", func(t *testing.T) {
		PatchConvey("validates before touching the table", t, func() {
			update := Mock((*ConfigService).updateConfigs).Return(nil).Build()

			So(svc.UpdateSchedulerExtenderConfig(t.Context(), nil), ShouldNotBeNil)
			zero := &UpdateSchedulerExtenderConfigReq{JobWaitingToleranceSeconds: ptr.To(int64(0))}
			So(svc.UpdateSchedulerExtenderConfig(t.Context(), zero), ShouldNotBeNil)
			So(svc.UpdateSchedulerExtenderConfig(t.Context(), &UpdateSchedulerExtenderConfigReq{}), ShouldBeNil)
			So(update.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("writes only the provided keys", func(t *testing.T) {
		PatchConvey("writes only the provided keys", t, func() {
			var written map[string]string
			Mock((*ConfigService).updateConfigs).To(func(_ *ConfigService, _ context.Context, updates map[string]string) error {
				written = updates
				return nil
			}).Build()

			req := &UpdateSchedulerExtenderConfigReq{QueueQuotaEnabled: ptr.To(true), JobWaitingToleranceSeconds: ptr.To(int64(600))}
			So(svc.UpdateSchedulerExtenderConfig(t.Context(), req), ShouldBeNil)
			So(written, ShouldResemble, map[string]string{
				model.ConfigKeyQueueQuotaEnabled:          "true",
				model.ConfigKeyJobWaitingToleranceSeconds: "600",
			})
		})
	})
}
