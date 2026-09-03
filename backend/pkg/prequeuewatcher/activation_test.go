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

package prequeuewatcher

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/go-logr/logr"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/datatypes"
	"gorm.io/gen"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/utils/tests"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/internal/service"
	vcjobservice "github.com/raids-lab/crater/internal/service/vcjob"
	"github.com/raids-lab/crater/pkg/crclient"
)

const storedJobName = "stored-job"

// detachedWatcher builds gorm-gen statements over a dialector without a connection; every terminal
// call is stubbed, so no SQL ever executes.
func detachedWatcher(t *testing.T) *PrequeueWatcher {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return &PrequeueWatcher{q: query.Use(db), logger: logr.Discard()}
}

func stubTransaction() {
	Mock((*query.Query).Transaction).To(func(q *query.Query, fc func(*query.Query) error, _ ...*sql.TxOptions) error {
		return fc(q)
	}).Build()
}

func stubClaim(rowsAffected int64, err error) {
	Mock((*gen.DO).Updates).Return(gen.ResultInfo{RowsAffected: rowsAffected}, err).Build()
}

func storedRecord() *model.Job {
	job := &batch.Job{ObjectMeta: metav1.ObjectMeta{Name: storedJobName, UID: "old-uid", ResourceVersion: "9"}}
	job.Status.State.Phase = batch.Running
	return &model.Job{JobName: storedJobName, Status: model.Prequeue, Attributes: datatypes.NewJSONType(job)}
}

func TestClaimAndActivatePrequeueJob(t *testing.T) {
	candidate := &model.Job{JobName: storedJobName, Status: model.Prequeue}

	t.Run("claim failure", func(t *testing.T) {
		PatchConvey("claim failure", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(0, errors.New("db down"))
			restore := Mock((*PrequeueWatcher).restoreJobForActivation).Return(&batch.Job{}, nil).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeFalse)
			So(err, ShouldNotBeNil)
			So(restore.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("lost the claim race", func(t *testing.T) {
		PatchConvey("lost the claim race", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(0, nil)
			restore := Mock((*PrequeueWatcher).restoreJobForActivation).Return(&batch.Job{}, nil).Build()
			activate := Mock(vcjobservice.ActivateJob).Return(nil).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeFalse)
			So(err, ShouldBeNil)
			So(restore.MockTimes(), ShouldEqual, 0)
			So(activate.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("restore failure rolls back", func(t *testing.T) {
		PatchConvey("restore failure rolls back", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(1, nil)
			Mock((*PrequeueWatcher).restoreJobForActivation).Return(nil, errors.New("template broken")).Build()
			activate := Mock(vcjobservice.ActivateJob).Return(nil).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeFalse)
			So(err, ShouldNotBeNil)
			So(activate.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("already existing vcjob counts as submitted", func(t *testing.T) {
		PatchConvey("already existing vcjob counts as submitted", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(1, nil)
			Mock((*PrequeueWatcher).restoreJobForActivation).Return(&batch.Job{}, nil).Build()
			exists := apierrors.NewAlreadyExists(schema.GroupResource{Group: "batch.volcano.sh", Resource: "jobs"}, storedJobName)
			Mock(vcjobservice.ActivateJob).Return(exists).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeTrue)
			So(err, ShouldBeNil)
		})
	})

	t.Run("other submit errors roll back", func(t *testing.T) {
		PatchConvey("other submit errors roll back", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(1, nil)
			Mock((*PrequeueWatcher).restoreJobForActivation).Return(&batch.Job{}, nil).Build()
			submitErr := errors.New("apiserver down")
			Mock(vcjobservice.ActivateJob).Return(submitErr).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeFalse)
			So(errors.Is(err, submitErr), ShouldBeTrue)
		})
	})

	t.Run("submits under a deadline", func(t *testing.T) {
		PatchConvey("submits under a deadline", t, func() {
			w := detachedWatcher(t)
			stubTransaction()
			stubClaim(1, nil)
			Mock((*PrequeueWatcher).restoreJobForActivation).Return(&batch.Job{}, nil).Build()
			hasDeadline := false
			Mock(vcjobservice.ActivateJob).To(
				func(ctx context.Context, _ client.Client, _ crclient.ServiceManagerInterface, _ *batch.Job) error {
					_, hasDeadline = ctx.Deadline()
					return nil
				}).Build()

			activated, err := w.claimAndActivatePrequeueJob(t.Context(), candidate)
			So(activated, ShouldBeTrue)
			So(err, ShouldBeNil)
			So(hasDeadline, ShouldBeTrue)
		})
	})
}

func TestRestoreJobForActivation(t *testing.T) {
	w := &PrequeueWatcher{logger: logr.Discard()}

	t.Run("record without a stored template", func(t *testing.T) {
		PatchConvey("record without a stored template", t, func() {
			job, err := w.restoreJobForActivation(t.Context(), &model.Job{})
			So(job, ShouldBeNil)
			So(err, ShouldNotBeNil)

			job, err = w.restoreJobForActivation(t.Context(), nil)
			So(job, ShouldBeNil)
			So(err, ShouldNotBeNil)
		})
	})

	t.Run("bandwidth failure", func(t *testing.T) {
		PatchConvey("bandwidth failure", t, func() {
			bandwidthErr := errors.New("cni unavailable")
			Mock(service.ApplyJobPodBandwidth).Return(bandwidthErr).Build()

			job, err := w.restoreJobForActivation(t.Context(), storedRecord())
			So(job, ShouldBeNil)
			So(errors.Is(err, bandwidthErr), ShouldBeTrue)
		})
	})

	t.Run("rebuilds a fresh vcjob from the record", func(t *testing.T) {
		PatchConvey("rebuilds a fresh vcjob from the record", t, func() {
			var applied *batch.Job
			Mock(service.ApplyJobPodBandwidth).To(
				func(_ context.Context, _ *service.ConfigService, _ kubernetes.Interface, job *batch.Job) error {
					applied = job
					return nil
				}).Build()

			job, err := w.restoreJobForActivation(t.Context(), storedRecord())
			So(err, ShouldBeNil)
			So(job, ShouldEqual, applied)
			So(job.Name, ShouldEqual, storedJobName)
			So(job.UID, ShouldBeEmpty)
			So(job.ResourceVersion, ShouldBeEmpty)
			So(job.Status.State.Phase, ShouldBeEmpty)
		})
	})
}

func TestListPrequeueJobs(t *testing.T) {
	t.Run("queries the prequeue backlog oldest first", func(t *testing.T) {
		PatchConvey("queries the prequeue backlog oldest first", t, func() {
			w := detachedWatcher(t)
			fixture := []*model.Job{{JobName: "first"}, {JobName: "second"}}
			var stmt *gorm.Statement
			Mock((*gorm.DB).Find).To(func(db *gorm.DB, dest any, _ ...any) *gorm.DB {
				stmt = db.Statement
				*dest.(*[]*model.Job) = fixture
				return db
			}).Build()

			records, err := w.listPrequeueJobs(t.Context(), maxSubmitsPerRound)
			So(err, ShouldBeNil)
			So(records, ShouldResemble, fixture)

			where, ok := stmt.Clauses["WHERE"].Expression.(clause.Where)
			So(ok, ShouldBeTrue)
			So(where.Exprs, ShouldHaveLength, 1)
			So(where.Exprs[0], ShouldResemble, clause.Expr{SQL: "status = ?", Vars: []any{model.Prequeue}})
			order, ok := stmt.Clauses["ORDER BY"].Expression.(clause.OrderBy)
			So(ok, ShouldBeTrue)
			So(order.Columns[0].Column.Name, ShouldEqual, "creation_timestamp ASC")
			limit, ok := stmt.Clauses["LIMIT"].Expression.(clause.Limit)
			So(ok, ShouldBeTrue)
			So(*limit.Limit, ShouldEqual, maxSubmitsPerRound)
		})
	})
}
