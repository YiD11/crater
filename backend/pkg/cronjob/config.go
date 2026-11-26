package cronjob

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"
	"k8s.io/klog/v2"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/raids-lab/crater/dao/model"
	"github.com/raids-lab/crater/dao/query"
	"github.com/raids-lab/crater/pkg/cleaner"
	"github.com/raids-lab/crater/pkg/patrol"
)

// buildEntry validates the spec and builds the callback without touching the scheduler
func (cm *CronJobManager) buildEntry(
	jobName string,
	jobSpec string,
	jobType model.CronJobType,
	jobConfig datatypes.JSON,
) (cron.Schedule, cron.FuncJob, error) {
	schedule, err := cron.ParseStandard(jobSpec)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid cron spec %q for job %s: %w", jobSpec, jobName, err)
	}
	f, err := cm.newCronJobFunc(jobName, jobType, jobConfig)
	if err != nil {
		return nil, nil, err
	}
	return schedule, f, nil
}

// replaceEntry swaps the schedule of a job; the caller must hold cronMutex
func (cm *CronJobManager) replaceEntry(jobName string, schedule cron.Schedule, f cron.FuncJob) {
	cm.removeEntry(jobName)
	cm.entries[jobName] = cm.cron.Schedule(schedule, f)
}

// removeEntry drops the schedule of a job if this process holds one; the caller must hold cronMutex
func (cm *CronJobManager) removeEntry(jobName string) {
	if entryID, ok := cm.entries[jobName]; ok {
		cm.cron.Remove(entryID)
		delete(cm.entries, jobName)
	}
}

// newCronJobFunc creates the appropriate cron job function based on job name
func (cm *CronJobManager) newCronJobFunc(jobName string, jobType model.CronJobType, jobConfig datatypes.JSON) (cron.FuncJob, error) {
	switch jobType {
	case model.CronJobTypeCleanerFunc:
		f, err := cleaner.GetCleanerFunc(jobName, cm.cleanerClients, jobConfig)
		if err != nil {
			err := fmt.Errorf("newCronJobFunc failed to get cleaner func: %w", err)
			klog.Error(err)
			return nil, err
		}
		return WrapFunc(jobName, f), nil
	case model.CronJobTypePatrolFunc:
		f, err := patrol.GetPatrolFunc(jobName, cm.patrolClients, jobConfig)
		if err != nil {
			err := fmt.Errorf("newCronJobFunc failed to get patrol func: %w", err)
			klog.Error(err)
			return nil, err
		}
		return WrapFunc(jobName, f), nil
	default:
		return nil, fmt.Errorf("unsupported cron job type: %s", jobType)
	}
}

// UpdateJobConfig updates the configuration of an existing cron job
func (cm *CronJobManager) UpdateJobConfig(
	_ *gin.Context,
	name string,
	jobType *model.CronJobType,
	spec *string,
	status *model.CronJobConfigStatus,
	config *string,
) error {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()

	var (
		schedule cron.Schedule
		f        cron.FuncJob
		suspend  bool
	)

	err := query.GetDB().Transaction(func(tx *gorm.DB) error {
		cur, err := cm.getCurrentJobConfigFromDB(tx, name)
		if err != nil {
			return err
		}

		update := cm.prepareUpdateConfig(cur, jobType, spec, status, config)
		suspend = update.IsSuspended()
		if !suspend {
			if schedule, f, err = cm.buildEntry(name, update.Spec, update.Type, update.Config); err != nil {
				klog.Error(err)
				return err
			}
			if status != nil || update.Status == model.CronJobConfigStatusUnknown {
				update.Status = model.CronJobConfigStatusIdle
			}
		}
		return cm.saveJobConfig(tx, cur, update)
	})
	if err != nil {
		return err
	}

	// The scheduler is only touched after the commit, and neither call can fail, so it cannot diverge from the database
	if suspend {
		cm.removeEntry(name)
	} else {
		cm.replaceEntry(name, schedule, f)
	}
	return nil
}

// getCurrentJobConfigFromDB retrieves current job configuration from database with row-level lock
func (cm *CronJobManager) getCurrentJobConfigFromDB(tx *gorm.DB, name string) (*model.CronJobConfig, error) {
	cur := &model.CronJobConfig{}
	if txErr := tx.
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Model(cur).
		Where(query.CronJobConfig.Name.Eq(name)).
		First(cur).Error; txErr != nil {
		err := fmt.Errorf("CronJobManager.getCurrentJobConfigFromDB failed: %w", txErr)
		klog.Error(err)
		return nil, err
	}
	return cur, nil
}

// prepareUpdateConfig creates update configuration
func (cm *CronJobManager) prepareUpdateConfig(
	cur *model.CronJobConfig,
	jobType *model.CronJobType,
	spec *string,
	status *model.CronJobConfigStatus,
	config *string,
) *model.CronJobConfig {
	update := &model.CronJobConfig{
		Name:   cur.Name,
		Type:   cur.Type,
		Spec:   cur.Spec,
		Config: cur.Config,
		Status: cur.Status,
	}
	if jobType != nil {
		update.Type = *jobType
	}
	if spec != nil && *spec != "" {
		update.Spec = *spec
	}
	if status != nil {
		update.Status = *status
	}
	if config != nil && *config != "" {
		update.Config = datatypes.JSON(*config)
	}
	return update
}

// saveJobConfig persists the update; EntryID stays zero so that gorm leaves the column alone
func (cm *CronJobManager) saveJobConfig(tx *gorm.DB, cur, update *model.CronJobConfig) error {
	if err := tx.Model(cur).Where(query.CronJobConfig.Name.Eq(cur.Name)).Updates(update).Error; err != nil {
		err := fmt.Errorf("DB failed to update cron job config for job %s: %w", cur.Name, err)
		klog.Error(err)
		return err
	}
	return nil
}

// SyncCronJob synchronizes cron jobs from database and starts the scheduler
func (cm *CronJobManager) SyncCronJob() {
	db := query.GetDB()
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()
	// Started last so that nothing fires before the statuses below are reset
	defer cm.cron.Start()

	var configs []*model.CronJobConfig
	if err := db.Where(query.CronJobConfig.Status.Neq(string(model.CronJobConfigStatusSuspended))).Find(&configs).Error; err != nil {
		klog.Errorf("CronJobManager.SyncCronJob: failed to load cron job configs: %v", err)
		return
	}
	klog.Infof("CronJobManager.SyncCronJob: loaded %d non-suspended cron jobs from database", len(configs))

	scheduled := make([]string, 0, len(configs))
	for _, conf := range configs {
		schedule, f, err := cm.buildEntry(conf.Name, conf.Spec, conf.Type, conf.Config)
		if err != nil {
			klog.Errorf("CronJobManager.SyncCronJob: failed to add cron job %s with spec %s: %v", conf.Name, conf.Spec, err)
			continue
		}
		cm.replaceEntry(conf.Name, schedule, f)
		scheduled = append(scheduled, conf.Name)
	}

	if len(scheduled) > 0 {
		if err := db.Model(&model.CronJobConfig{}).
			Where(query.CronJobConfig.Name.In(scheduled...)).
			Where(query.CronJobConfig.Status.Neq(string(model.CronJobConfigStatusSuspended))).
			Update(query.CronJobConfig.Status.ColumnName().String(), model.CronJobConfigStatusIdle).Error; err != nil {
			klog.Errorf("CronJobManager.SyncCronJob: failed to reset job status: %v", err)
		}
	}
	klog.Infof("CronJobManager.SyncCronJob: scheduled %d cron jobs", len(scheduled))
}

// StopCron stops the cron scheduler
func (cm *CronJobManager) StopCron() {
	cm.cronMutex.Lock()
	defer cm.cronMutex.Unlock()
	cm.cron.Stop()
}
