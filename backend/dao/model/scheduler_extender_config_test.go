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

package model

import (
	"testing"

	// mockey is not dot-imported here: its exported Private would clash with model.Private.
	. "github.com/smartystreets/goconvey/convey"
)

func TestSchedulerExtenderConfig(t *testing.T) {
	t.Run("defaults keep the extender off", func(t *testing.T) {
		Convey("defaults keep the extender off", t, func() {
			cfg := NewSchedulerExtenderConfig()
			So(cfg.SchedulerExtenderEnabled, ShouldBeFalse)
			So(cfg.QueueQuotaEnabled, ShouldBeFalse)
			So(cfg.JobWaitingToleranceSeconds, ShouldEqual, DefaultJobWaitingToleranceSeconds)
			So(cfg.Validate(), ShouldBeNil)
		})
	})

	t.Run("validation rejects nil and non-positive tolerance", func(t *testing.T) {
		Convey("validation rejects nil and non-positive tolerance", t, func() {
			var nilCfg *SchedulerExtenderConfig
			So(nilCfg.Validate(), ShouldNotBeNil)
			So((&SchedulerExtenderConfig{JobWaitingToleranceSeconds: 0}).Validate(), ShouldNotBeNil)
			So((&SchedulerExtenderConfig{JobWaitingToleranceSeconds: -1}).Validate(), ShouldNotBeNil)
			So((&SchedulerExtenderConfig{JobWaitingToleranceSeconds: 1}).Validate(), ShouldBeNil)
		})
	})
}
