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

package extender

import (
	"context"
	"errors"
	"testing"
	"time"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	vcjobservice "github.com/raids-lab/crater/internal/service/vcjob"
	"github.com/raids-lab/crater/pkg/config"
	"github.com/raids-lab/crater/pkg/utils"
)

const (
	jobAdmitted = "job-admitted"
	jobPending  = "job-pending"
)

type failingReader struct {
	client.Reader
	err error
}

func (r failingReader) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return r.err
}

func groupWithPhase(phase scheduling.PodGroupPhase) *scheduling.PodGroup {
	return &scheduling.PodGroup{Status: scheduling.PodGroupStatus{Phase: phase}}
}

func TestNewJobView(t *testing.T) {
	t.Run("waiting job past its tolerance", func(t *testing.T) {
		PatchConvey("waiting job past its tolerance", t, func() {
			view := newJobView(newVCJob(jobA, testNamespace, rl("cpu", "1")), nil, fixedNow)
			So(view.name, ShouldEqual, jobA)
			So(view.userID, ShouldEqual, testUserID)
			So(view.queue, ShouldEqual, publicQueue)
			So(view.resources.Cpu().String(), ShouldEqual, "1")
			So(view.minimum.Cpu().String(), ShouldEqual, "1")
			So(view.domain, ShouldEqual, utils.ResourceDomainCPUOnly)
			So(view.admitted, ShouldBeFalse)
			So(view.terminal, ShouldBeFalse)
			So(view.waiting, ShouldBeTrue)
			So(view.timedOut, ShouldBeTrue)
		})
	})

	t.Run("tolerance boundary is exclusive", func(t *testing.T) {
		PatchConvey("tolerance boundary is exclusive", t, func() {
			job := newVCJob(jobA, testNamespace, rl("cpu", "1"), func(job *batch.Job) {
				job.CreationTimestamp = metav1.NewTime(fixedNow.Add(-300 * time.Second))
			})
			So(newJobView(job, nil, fixedNow).timedOut, ShouldBeFalse)
			So(newJobView(job, nil, fixedNow.Add(time.Second)).timedOut, ShouldBeTrue)
		})
	})

	t.Run("missing tolerance never times out", func(t *testing.T) {
		PatchConvey("missing tolerance never times out", t, func() {
			job := newVCJob(jobA, testNamespace, rl("cpu", "1"), func(job *batch.Job) {
				delete(job.Annotations, vcjobservice.AnnotationKeyWaitingToleranceSeconds)
			})
			So(newJobView(job, nil, fixedNow).timedOut, ShouldBeFalse)
		})
	})

	t.Run("terminal and running jobs are not waiting", func(t *testing.T) {
		PatchConvey("terminal and running jobs are not waiting", t, func() {
			completed := newVCJob(jobA, testNamespace, rl("cpu", "1"), func(job *batch.Job) {
				job.Status.State.Phase = batch.Completed
			})
			view := newJobView(completed, nil, fixedNow)
			So(view.terminal, ShouldBeTrue)
			So(view.waiting, ShouldBeFalse)
			So(view.timedOut, ShouldBeFalse)

			running := newVCJob(jobA, testNamespace, rl("cpu", "1"), func(job *batch.Job) {
				job.Status.State.Phase = batch.Running
			})
			view = newJobView(running, nil, fixedNow)
			So(view.terminal, ShouldBeFalse)
			So(view.waiting, ShouldBeFalse)
			So(view.timedOut, ShouldBeFalse)
		})
	})

	t.Run("pod group supplies admission and minimum demand", func(t *testing.T) {
		PatchConvey("pod group supplies admission and minimum demand", t, func() {
			job := newVCJob(jobA, testNamespace, rl("cpu", "1"))
			group := newPodGroup(job, scheduling.PodGroupInqueue)
			group.Spec.MinResources = ptr.To(rl("cpu", "2"))
			view := newJobView(job, group, fixedNow)
			So(view.admitted, ShouldBeTrue)
			So(view.minimum.Cpu().String(), ShouldEqual, "2")
			So(view.resources.Cpu().String(), ShouldEqual, "1")
			So(view.waiting, ShouldBeTrue)
			So(view.occupiesQuota(), ShouldBeTrue)
		})
	})
}

func TestPhaseHelpers(t *testing.T) {
	t.Run("isAdmitted", func(t *testing.T) {
		PatchConvey("isAdmitted", t, func() {
			So(isAdmitted(nil), ShouldBeFalse)
			So(isAdmitted(groupWithPhase("")), ShouldBeFalse)
			So(isAdmitted(groupWithPhase(scheduling.PodGroupPending)), ShouldBeFalse)
			So(isAdmitted(groupWithPhase(scheduling.PodGroupInqueue)), ShouldBeTrue)
			So(isAdmitted(groupWithPhase(scheduling.PodGroupRunning)), ShouldBeTrue)
		})
	})

	t.Run("isTerminalPhase", func(t *testing.T) {
		PatchConvey("isTerminalPhase", t, func() {
			for _, phase := range []batch.JobPhase{batch.Completed, batch.Failed, batch.Aborted, batch.Terminated} {
				So(isTerminalPhase(phase), ShouldBeTrue)
			}
			for _, phase := range []batch.JobPhase{"", batch.Pending, batch.Running, batch.Completing, batch.Terminating} {
				So(isTerminalPhase(phase), ShouldBeFalse)
			}
		})
	})
}

func TestAnnotationParsers(t *testing.T) {
	t.Run("annotationUserID", func(t *testing.T) {
		PatchConvey("annotationUserID", t, func() {
			So(annotationUserID(nil), ShouldEqual, 0)
			for _, raw := range []string{"", "abc", "-1"} {
				So(annotationUserID(map[string]string{vcjobservice.AnnotationKeyUserID: raw}), ShouldEqual, 0)
			}
			So(annotationUserID(map[string]string{vcjobservice.AnnotationKeyUserID: "42"}), ShouldEqual, 42)
		})
	})

	t.Run("annotationTolerance", func(t *testing.T) {
		PatchConvey("annotationTolerance", t, func() {
			So(annotationTolerance(nil), ShouldBeNil)
			for _, raw := range []string{"", "abc", "0", "-5"} {
				So(annotationTolerance(map[string]string{vcjobservice.AnnotationKeyWaitingToleranceSeconds: raw}), ShouldBeNil)
			}
			tolerance := annotationTolerance(map[string]string{vcjobservice.AnnotationKeyWaitingToleranceSeconds: "300"})
			So(tolerance, ShouldNotBeNil)
			So(*tolerance, ShouldEqual, 5*time.Minute)
		})
	})
}

func TestPodGroupOwnership(t *testing.T) {
	t.Run("controllerOwnerJobName", func(t *testing.T) {
		PatchConvey("controllerOwnerJobName", t, func() {
			job := newVCJob(jobA, testNamespace, rl("cpu", "1"))
			owner := vcjobOwnerRef(job)
			So(controllerOwnerJobName(nil), ShouldBeEmpty)
			So(controllerOwnerJobName([]metav1.OwnerReference{owner}), ShouldEqual, jobA)

			for _, mutate := range []func(*metav1.OwnerReference){
				func(ref *metav1.OwnerReference) { ref.Controller = nil },
				func(ref *metav1.OwnerReference) { ref.Controller = ptr.To(false) },
				func(ref *metav1.OwnerReference) { ref.Kind = "Deployment" },
				func(ref *metav1.OwnerReference) { ref.APIVersion = "apps/v1" },
			} {
				ref := owner
				mutate(&ref)
				So(controllerOwnerJobName([]metav1.OwnerReference{ref}), ShouldBeEmpty)
			}
		})
	})

	t.Run("podGroupsByOwnerJob", func(t *testing.T) {
		PatchConvey("podGroupsByOwnerJob", t, func() {
			job := newVCJob(jobA, testNamespace, rl("cpu", "1"))
			first := newPodGroup(job, scheduling.PodGroupPending)
			second := newPodGroup(job, scheduling.PodGroupInqueue)
			second.Name = jobA + "-second"
			orphan := &scheduling.PodGroup{ObjectMeta: metav1.ObjectMeta{Name: "podgroup-plain-pod"}}

			groups := podGroupsByOwnerJob(&scheduling.PodGroupList{Items: []scheduling.PodGroup{*first, *second, *orphan}})
			So(len(groups), ShouldEqual, 1)
			So(groups[jobA].Name, ShouldEqual, second.Name)
		})
	})
}

func TestBuildSnapshot(t *testing.T) {
	t.Run("joins jobs, pod groups and queues", func(t *testing.T) {
		PatchConvey("joins jobs, pod groups and queues", t, func() {
			cfg := &config.Config{}
			cfg.Namespaces.Job = testNamespace
			Mock(config.GetConfig).Return(cfg).Build()
			Mock(utils.GetLocalTime).Return(fixedNow).Build()

			admittedJob := newVCJob(jobAdmitted, testNamespace, rl("cpu", "2"))
			// Created 100s ago with a 300s tolerance: only the frozen clock keeps it from timing out.
			pendingJob := newVCJob(jobPending, testNamespace, rl("cpu", "1"), func(job *batch.Job) {
				job.CreationTimestamp = metav1.NewTime(fixedNow.Add(-100 * time.Second))
			})
			foreignJob := newVCJob("job-foreign", "other-namespace", rl("cpu", "1"))
			reader := fake.NewClientBuilder().WithScheme(volcanoScheme()).WithObjects(
				admittedJob, pendingJob, foreignJob,
				newPodGroup(admittedJob, scheduling.PodGroupInqueue),
				newPodGroup(pendingJob, scheduling.PodGroupPending),
				&scheduling.Queue{ObjectMeta: metav1.ObjectMeta{Name: publicQueue}},
			).Build()

			snap, err := (&Server{reader: reader}).buildSnapshot(t.Context(), &settings{})
			So(err, ShouldBeNil)
			So(len(snap.views), ShouldEqual, 2)
			So(snap.byName, ShouldNotContainKey, "job-foreign")
			So(snap.byName[jobAdmitted].admitted, ShouldBeTrue)
			So(snap.byName[jobPending].admitted, ShouldBeFalse)
			So(snap.byName[jobPending].timedOut, ShouldBeFalse)
			So(snap.queues, ShouldContainKey, publicQueue)
			So(snap.quotas, ShouldBeNil)
			usage := snap.usageByOwner[ownerKey{userID: testUserID, queue: publicQueue}]
			So(usage.Cpu().String(), ShouldEqual, "2")
		})
	})

	t.Run("propagates cache read errors", func(t *testing.T) {
		PatchConvey("propagates cache read errors", t, func() {
			cfg := &config.Config{}
			cfg.Namespaces.Job = testNamespace
			Mock(config.GetConfig).Return(cfg).Build()
			listErr := errors.New("cache not started")
			reader := failingReader{Reader: fake.NewClientBuilder().WithScheme(volcanoScheme()).Build(), err: listErr}

			snap, err := (&Server{reader: reader}).buildSnapshot(t.Context(), &settings{})
			So(snap, ShouldBeNil)
			So(errors.Is(err, listErr), ShouldBeTrue)
		})
	})
}
