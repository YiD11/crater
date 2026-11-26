package cronjob

import (
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/raids-lab/crater/pkg/cleaner"
	"github.com/raids-lab/crater/pkg/monitor"
	"github.com/raids-lab/crater/pkg/patrol"
)

type CronJobManager struct {
	Client         client.Client
	KubeClient     kubernetes.Interface
	PromClient     monitor.PrometheusInterface
	cleanerClients *cleaner.Clients
	patrolClients  *patrol.Clients
	cron           *cron.Cron
	cronMutex      sync.RWMutex
	// robfig entry ids are process-local, so they are tracked here by job name and never taken from the database
	entries map[string]cron.EntryID
}

// ScheduleState describes what this process's scheduler actually holds for a job.
type ScheduleState struct {
	EntryID   cron.EntryID
	Scheduled bool
	NextRun   time.Time
}

func NewCronJobManager(
	cli client.Client,
	kubeClient kubernetes.Interface,
	promClient monitor.PrometheusInterface,
	gpuAnalysisService patrol.GpuAnalysisServiceInterface,
	billingService patrol.BillingServiceInterface,
) *CronJobManager {
	return &CronJobManager{
		Client:     cli,
		KubeClient: kubeClient,
		PromClient: promClient,
		cleanerClients: &cleaner.Clients{
			Client:     cli,
			KubeClient: kubeClient,
			PromClient: promClient,
		},
		patrolClients: &patrol.Clients{
			Client:             cli,
			KubeClient:         kubeClient,
			PromClient:         promClient,
			GpuAnalysisService: gpuAnalysisService,
			BillingService:     billingService,
		},
		cron:    cron.New(cron.WithLocation(time.Local)),
		entries: make(map[string]cron.EntryID),
	}
}

func (cm *CronJobManager) GetScheduleState(name string) ScheduleState {
	cm.cronMutex.RLock()
	defer cm.cronMutex.RUnlock()

	id, ok := cm.entries[name]
	if !ok {
		return ScheduleState{}
	}
	entry := cm.cron.Entry(id)
	return ScheduleState{EntryID: id, Scheduled: entry.Valid(), NextRun: entry.Next}
}

// Timezone reports the offset cron specs are evaluated in; time.Local only ever names itself "Local".
func (cm *CronJobManager) Timezone() string {
	return "UTC" + time.Now().In(cm.cron.Location()).Format("-07:00")
}
