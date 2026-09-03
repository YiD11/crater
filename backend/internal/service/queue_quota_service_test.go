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
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/datatypes"
	"gorm.io/gen"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
	v1 "k8s.io/api/core/v1"
	apiresource "k8s.io/apimachinery/pkg/api/resource"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/bizerr"
)

const (
	quotaQueue   = "default"
	privateQueue = "q-a2-u9"
	cpuName      = "cpu"
)

func quotaList(pairs ...string) v1.ResourceList {
	list := v1.ResourceList{}
	for i := 0; i+1 < len(pairs); i += 2 {
		list[v1.ResourceName(pairs[i])] = apiresource.MustParse(pairs[i+1])
	}
	return list
}

func enabledQuota(quota map[string]string) *ResolvedQueueQuota {
	return &ResolvedQueueQuota{Name: quotaQueue, Enabled: true, Quota: quota}
}

// detachedQuotaService builds gorm-gen statements over a dialector without a connection; the
// terminal Find is stubbed so nothing executes.
func detachedQuotaService(t *testing.T) *QueueQuotaService {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return NewQueueQuotaService(query.Use(db), nil)
}

func TestEvaluateQueueQuota(t *testing.T) {
	t.Run("disabled or empty quotas are not enforced", func(t *testing.T) {
		PatchConvey("disabled or empty quotas are not enforced", t, func() {
			So(EvaluateQueueQuota(nil, nil, nil).Enabled, ShouldBeFalse)
			disabled := &ResolvedQueueQuota{Name: quotaQueue, Quota: map[string]string{cpuName: "1"}}
			So(EvaluateQueueQuota(disabled, nil, nil).Enabled, ShouldBeFalse)
			So(EvaluateQueueQuota(enabledQuota(map[string]string{}), nil, nil).Enabled, ShouldBeFalse)
		})
	})

	t.Run("only strictly above the limit is exceeded", func(t *testing.T) {
		PatchConvey("only strictly above the limit is exceeded", t, func() {
			quota := enabledQuota(map[string]string{cpuName: "2"})
			result := EvaluateQueueQuota(quota, quotaList(cpuName, "2"), quotaList(cpuName, "1"))
			So(result.Enabled, ShouldBeTrue)
			So(result.Exceeded, ShouldBeTrue)
			So(result.Details, ShouldResemble, []ResourceLimitDetail{{Resource: cpuName, Used: "3", Limit: "2", Exceeded: true}})

			equal := EvaluateQueueQuota(quota, quotaList(cpuName, "1"), quotaList(cpuName, "1"))
			So(equal.Exceeded, ShouldBeFalse)
			So(equal.Details[0].Used, ShouldEqual, "2")
		})
	})

	t.Run("resources outside the quota are ignored", func(t *testing.T) {
		PatchConvey("resources outside the quota are ignored", t, func() {
			quota := enabledQuota(map[string]string{cpuName: "2"})
			result := EvaluateQueueQuota(quota, quotaList("memory", "10Gi"), quotaList(cpuName, "1", "memory", "10Gi"))
			So(result.Exceeded, ShouldBeFalse)
			So(result.Details, ShouldHaveLength, 1)
		})
	})

	t.Run("unparsable values are skipped", func(t *testing.T) {
		PatchConvey("unparsable values are skipped", t, func() {
			quota := enabledQuota(map[string]string{cpuName: "lots", "memory": "1Gi"})
			result := EvaluateQueueQuota(quota, quotaList(cpuName, "100"), nil)
			So(result.Exceeded, ShouldBeFalse)
			So(result.Details, ShouldHaveLength, 1)
			So(result.Details[0].Resource, ShouldEqual, "memory")

			projected := map[string]int64{}
			addMilliQuantities(projected, map[string]string{cpuName: "1500m", "memory": "garbage"})
			So(projected, ShouldResemble, map[string]int64{cpuName: 1500})
		})
	})
}

func TestQueueQuotaOccupiedJobPhases(t *testing.T) {
	t.Run("counts admitted jobs only", func(t *testing.T) {
		PatchConvey("counts admitted jobs only", t, func() {
			phases := QueueQuotaOccupiedJobPhases()
			So(phases, ShouldContain, string(batch.Running))
			So(phases, ShouldContain, string(model.Inqueue))
			So(phases, ShouldNotContain, string(batch.Pending))
			So(phases, ShouldNotContain, string(model.Prequeue))
		})
	})
}

func TestBuildUserResourceUsageSummary(t *testing.T) {
	t.Run("splits admitted usage into running and pending", func(t *testing.T) {
		PatchConvey("splits admitted usage into running and pending", t, func() {
			jobs := []*model.Job{
				{Queue: quotaQueue, Status: batch.Running, Resources: datatypes.NewJSONType(quotaList(cpuName, "2"))},
				{Queue: quotaQueue, Status: model.Inqueue, Resources: datatypes.NewJSONType(quotaList(cpuName, "1"))},
				{Queue: quotaQueue, Status: batch.Pending, Resources: datatypes.NewJSONType(quotaList(cpuName, "4"))},
				{Queue: privateQueue, Status: batch.Running, Resources: datatypes.NewJSONType(quotaList(cpuName, "8"))},
				nil,
			}
			summary := buildUserResourceUsageSummary(enabledQuota(map[string]string{cpuName: "4"}), jobs)
			So(summary.RunningJobs, ShouldEqual, 1)
			So(summary.PendingJobs, ShouldEqual, 1)
			cpu := summary.Resources[cpuName]
			So(cpu.Used, ShouldEqual, "3")
			So(cpu.Running, ShouldEqual, "2")
			So(cpu.Pending, ShouldEqual, "1")
			So(cpu.Limit, ShouldEqual, "4")
			So(cpu.HasLimit, ShouldBeTrue)

			empty := buildUserResourceUsageSummary(nil, jobs)
			So(empty.RunningJobs, ShouldEqual, 0)
			So(empty.Resources, ShouldBeEmpty)
		})
	})
}

func TestLoadQuotaSet(t *testing.T) {
	t.Run("sanitizes stored limits", func(t *testing.T) {
		PatchConvey("sanitizes stored limits", t, func() {
			svc := detachedQuotaService(t)
			Mock((*gen.DO).Find).Return([]*model.QueueQuotaLimit{{
				Name:  quotaQueue,
				Quota: datatypes.NewJSONType(map[string]string{" " + cpuName + " ": " 4 ", "": "1", "memory": " "}),
			}}, nil).Build()

			set, err := svc.LoadQuotaSet(t.Context(), &model.SchedulerExtenderConfig{QueueQuotaEnabled: true})
			So(err, ShouldBeNil)
			resolved := set.Resolve(quotaQueue)
			So(resolved.Enabled, ShouldBeTrue)
			So(resolved.Quota, ShouldResemble, map[string]string{cpuName: "4"})

			unknown := set.Resolve(privateQueue)
			So(unknown.Enabled, ShouldBeFalse)
			So(unknown.Quota, ShouldBeEmpty)
		})
	})

	t.Run("quota switch off disables every queue", func(t *testing.T) {
		PatchConvey("quota switch off disables every queue", t, func() {
			svc := detachedQuotaService(t)
			Mock((*gen.DO).Find).Return([]*model.QueueQuotaLimit{{
				Name:  quotaQueue,
				Quota: datatypes.NewJSONType(map[string]string{cpuName: "4"}),
			}}, nil).Build()

			set, err := svc.LoadQuotaSet(t.Context(), nil)
			So(err, ShouldBeNil)
			So(set.Enabled, ShouldBeFalse)
			So(set.Resolve(quotaQueue).Enabled, ShouldBeFalse)

			var nilSet *QueueQuotaSet
			So(nilSet.Resolve(quotaQueue).Enabled, ShouldBeFalse)
		})
	})

	t.Run("query failure", func(t *testing.T) {
		PatchConvey("query failure", t, func() {
			svc := detachedQuotaService(t)
			// gen.DO.Find never returns a nil result, and the generated wrapper type-asserts it unconditionally.
			Mock((*gen.DO).Find).Return([]*model.QueueQuotaLimit{}, bizerr.Internal.DatabaseError.New("db down")).Build()

			set, err := svc.LoadQuotaSet(t.Context(), &model.SchedulerExtenderConfig{QueueQuotaEnabled: true})
			So(set, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestCheckRequestedResourceLimit(t *testing.T) {
	svc := &QueueQuotaService{}

	t.Run("judges the request on its own", func(t *testing.T) {
		PatchConvey("judges the request on its own", t, func() {
			Mock((*QueueQuotaService).ResolveQueueQuota).Return(enabledQuota(map[string]string{cpuName: "2"}), nil).Build()

			result, err := svc.CheckRequestedResourceLimit(t.Context(), 7, 1, quotaQueue, map[string]string{cpuName: "3"})
			So(err, ShouldBeNil)
			So(result.Exceeded, ShouldBeTrue)

			result, err = svc.CheckRequestedResourceLimit(t.Context(), 7, 1, quotaQueue, map[string]string{cpuName: "2"})
			So(err, ShouldBeNil)
			So(result.Exceeded, ShouldBeFalse)
		})
	})

	t.Run("disabled quota and resolve failures", func(t *testing.T) {
		PatchConvey("disabled quota", t, func() {
			Mock((*QueueQuotaService).ResolveQueueQuota).Return(&ResolvedQueueQuota{Name: quotaQueue}, nil).Build()
			result, err := svc.CheckRequestedResourceLimit(t.Context(), 7, 1, quotaQueue, map[string]string{cpuName: "3"})
			So(err, ShouldBeNil)
			So(result.Enabled, ShouldBeFalse)
		})
		PatchConvey("resolve failure", t, func() {
			Mock((*QueueQuotaService).ResolveQueueQuota).Return(nil, bizerr.Internal.DatabaseError.New("db down")).Build()
			result, err := svc.CheckRequestedResourceLimit(t.Context(), 7, 1, quotaQueue, nil)
			So(result, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})
}
