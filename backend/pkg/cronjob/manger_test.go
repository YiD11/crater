package cronjob

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	. "github.com/bytedance/mockey"
	"github.com/robfig/cron/v3"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
	"k8s.io/utils/ptr"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/pkg/cleaner"
)

const (
	jobA = cleaner.CLEAN_LONG_TIME_RUNNING_JOB
	jobB = cleaner.CLEAN_WAITING_JUPYTER_JOB
)

var jobConfigs = map[string]datatypes.JSON{
	jobA: datatypes.JSON(`{"batchDays": 4, "interactiveDays": 4}`),
	jobB: datatypes.JSON(`{"waitMinitues": 5}`),
}

// scheduledManager holds a real, never started scheduler with jobA and jobB registered.
func scheduledManager(t *testing.T) (manager *CronJobManager, idA, idB cron.EntryID) {
	t.Helper()
	manager = NewCronJobManager(nil, nil, nil, nil, nil)
	for _, name := range []string{jobA, jobB} {
		schedule, f, err := manager.buildEntry(name, "*/5 * * * *", model.CronJobTypeCleanerFunc, jobConfigs[name])
		if err != nil {
			t.Fatal(err)
		}
		manager.replaceEntry(name, schedule, f)
	}
	return manager, manager.entries[jobA], manager.entries[jobB]
}

// stubDB runs transactions over a dialector without a connection; every terminal call is stubbed by the caller.
func stubDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	query.SetDefault(db)
	Mock(query.GetDB).Return(db).Build()
	Mock((*gorm.DB).Transaction).To(func(tx *gorm.DB, fc func(*gorm.DB) error, _ ...*sql.TxOptions) error {
		return fc(tx)
	}).Build()
}

func storedJobB(status model.CronJobConfigStatus, entryID cron.EntryID) *model.CronJobConfig {
	return &model.CronJobConfig{
		Name:    jobB,
		Type:    model.CronJobTypeCleanerFunc,
		Spec:    "*/5 * * * *",
		Config:  jobConfigs[jobB],
		Status:  status,
		EntryID: int(entryID),
	}
}

func TestUpdateJobConfig(t *testing.T) {
	PatchConvey("a stale entry_id in the database cannot remove another job", t, func() {
		manager, idA, idB := scheduledManager(t)
		stubDB(t)
		Mock((*CronJobManager).getCurrentJobConfigFromDB).Return(storedJobB(model.CronJobConfigStatusIdle, idA), nil).Build()
		var saved *model.CronJobConfig
		Mock((*CronJobManager).saveJobConfig).To(func(_ *CronJobManager, _ *gorm.DB, _, update *model.CronJobConfig) error {
			saved = update
			return nil
		}).Build()

		err := manager.UpdateJobConfig(nil, jobB, nil, ptr.To("0 16 * * *"), nil, nil)

		So(err, ShouldBeNil)
		So(saved.Spec, ShouldEqual, "0 16 * * *")
		So(saved.EntryID, ShouldEqual, 0)
		So(manager.cron.Entry(idA).Valid(), ShouldBeTrue)
		So(manager.cron.Entry(idB).Valid(), ShouldBeFalse)
		So(manager.cron.Entries(), ShouldHaveLength, 2)
		rescheduled := manager.cron.Entry(manager.entries[jobB])
		So(rescheduled.Valid(), ShouldBeTrue)
		So(rescheduled.Schedule.Next(time.Date(2026, 11, 26, 0, 0, 0, 0, time.Local)).Hour(), ShouldEqual, 16)
	})

	PatchConvey("a running job stays running unless a status is passed", t, func() {
		manager, _, idB := scheduledManager(t)
		stubDB(t)
		Mock((*CronJobManager).getCurrentJobConfigFromDB).Return(storedJobB(model.CronJobConfigStatusRunning, idB), nil).Build()
		var saved *model.CronJobConfig
		Mock((*CronJobManager).saveJobConfig).To(func(_ *CronJobManager, _ *gorm.DB, _, update *model.CronJobConfig) error {
			saved = update
			return nil
		}).Build()

		So(manager.UpdateJobConfig(nil, jobB, nil, ptr.To("0 16 * * *"), nil, nil), ShouldBeNil)
		So(saved.Status, ShouldEqual, model.CronJobConfigStatusRunning)

		So(manager.UpdateJobConfig(nil, jobB, nil, nil, ptr.To(model.CronJobConfigStatusIdle), nil), ShouldBeNil)
		So(saved.Status, ShouldEqual, model.CronJobConfigStatusIdle)
	})

	PatchConvey("an invalid spec keeps the old schedule and writes nothing", t, func() {
		manager, idA, idB := scheduledManager(t)
		stubDB(t)
		Mock((*CronJobManager).getCurrentJobConfigFromDB).Return(storedJobB(model.CronJobConfigStatusIdle, idB), nil).Build()
		save := Mock((*CronJobManager).saveJobConfig).Return(nil).Build()

		err := manager.UpdateJobConfig(nil, jobB, nil, ptr.To("not a cron spec"), ptr.To(model.CronJobConfigStatusIdle), nil)

		So(err, ShouldNotBeNil)
		So(save.MockTimes(), ShouldEqual, 0)
		So(manager.entries[jobB], ShouldEqual, idB)
		So(manager.cron.Entry(idB).Valid(), ShouldBeTrue)
		So(manager.cron.Entry(idA).Valid(), ShouldBeTrue)
	})

	PatchConvey("a failed write keeps the old schedule", t, func() {
		manager, _, idB := scheduledManager(t)
		stubDB(t)
		Mock((*CronJobManager).getCurrentJobConfigFromDB).Return(storedJobB(model.CronJobConfigStatusIdle, idB), nil).Build()
		Mock((*CronJobManager).saveJobConfig).Return(errors.New("db down")).Build()

		err := manager.UpdateJobConfig(nil, jobB, nil, ptr.To("0 16 * * *"), ptr.To(model.CronJobConfigStatusIdle), nil)

		So(err, ShouldNotBeNil)
		So(manager.entries[jobB], ShouldEqual, idB)
		So(manager.cron.Entries(), ShouldHaveLength, 2)
	})

	PatchConvey("suspending removes only that job", t, func() {
		manager, idA, idB := scheduledManager(t)
		stubDB(t)
		Mock((*CronJobManager).getCurrentJobConfigFromDB).Return(storedJobB(model.CronJobConfigStatusIdle, idA), nil).Build()
		Mock((*CronJobManager).saveJobConfig).Return(nil).Build()

		err := manager.UpdateJobConfig(nil, jobB, nil, nil, ptr.To(model.CronJobConfigStatusSuspended), nil)

		So(err, ShouldBeNil)
		So(manager.entries, ShouldNotContainKey, jobB)
		So(manager.cron.Entry(idB).Valid(), ShouldBeFalse)
		So(manager.cron.Entry(idA).Valid(), ShouldBeTrue)
		So(manager.GetScheduleState(jobB).Scheduled, ShouldBeFalse)
		So(manager.GetScheduleState(jobA).Scheduled, ShouldBeTrue)
	})
}

func TestWrapFunc(t *testing.T) {
	PatchConvey("a suspended job is not executed", t, func() {
		stubDB(t)
		Mock((*gorm.DB).First).To(func(tx *gorm.DB, dest any, _ ...any) *gorm.DB {
			dest.(*model.CronJobConfig).Status = model.CronJobConfigStatusSuspended
			return tx
		}).Build()
		markRunning := Mock((*gorm.DB).Update).To(func(tx *gorm.DB, _ string, _ any) *gorm.DB { return tx }).Build()
		executed := false

		WrapFunc(jobB, func(context.Context) (any, error) {
			executed = true
			return nil, nil
		})()

		So(executed, ShouldBeFalse)
		So(markRunning.MockTimes(), ShouldEqual, 0)
	})
}

func TestCronJob(t *testing.T) {
	t.Run("newCronJobFunc", func(t *testing.T) {
		manager := NewCronJobManager(nil, nil, nil, nil, nil)
		PatchConvey("newCronJobFunc", t, func() {
			jobName := cleaner.CLEAN_LONG_TIME_RUNNING_JOB
			jobConfig := datatypes.JSON(`{"batchDays": 4, "interactiveDays": 4}`)
			jobFunc, err := manager.newCronJobFunc(jobName, model.CronJobTypeCleanerFunc, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = cleaner.CLEAN_LOW_GPU_USAGE_JOB
			jobConfig = datatypes.JSON(`{"util": 0, "waitTime": 30, "timeRange": 90}`)
			jobFunc, err = manager.newCronJobFunc(jobName, model.CronJobTypeCleanerFunc, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = cleaner.CLEAN_WAITING_JUPYTER_JOB
			jobConfig = datatypes.JSON(`{"waitMinitues": 5}`)
			jobFunc, err = manager.newCronJobFunc(jobName, model.CronJobTypeCleanerFunc, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = cleaner.CLEAN_WAITING_CUSTOM_JOB
			jobConfig = datatypes.JSON(`{"waitMinitues": 5}`)
			jobFunc, err = manager.newCronJobFunc(jobName, model.CronJobTypeCleanerFunc, jobConfig)
			So(err, ShouldBeNil)
			So(jobFunc, ShouldNotBeNil)

			jobName = "unknown"
			jobConfig = datatypes.JSON(`{"unknown": "unknown"}`)
			jobFunc, err = manager.newCronJobFunc(jobName, model.CronJobTypeCleanerFunc, jobConfig)
			So(err, ShouldNotBeNil)
			So(jobFunc, ShouldBeNil)
		})
	})

	t.Run("prepareUpdateConfig", func(t *testing.T) {
		PatchConvey("prepareUpdateConfig", t, func() {
			manager := NewCronJobManager(nil, nil, nil, nil, nil)
			cur := &model.CronJobConfig{
				Name:   "test",
				Type:   model.CronJobTypeCleanerFunc,
				Spec:   "0 0 * * *",
				Status: model.CronJobConfigStatusIdle,
				Config: datatypes.JSON(`{"test": "test"}`),
			}
			update := manager.prepareUpdateConfig(
				cur,
				ptr.To(model.CronJobTypeCleanerFunc),
				ptr.To("1 1 * * *"),
				ptr.To(model.CronJobConfigStatusSuspended),
				ptr.To(`{"test": "test"}`),
			)
			So(update, ShouldNotBeNil)
			So(update.Name, ShouldEqual, "test")
			So(update.Type, ShouldEqual, model.CronJobTypeCleanerFunc)
			So(update.Spec, ShouldEqual, "1 1 * * *")
			So(update.Status, ShouldEqual, model.CronJobConfigStatusSuspended)
			So(update.Config, ShouldEqual, datatypes.JSON(`{"test": "test"}`))
		})
	})
}
