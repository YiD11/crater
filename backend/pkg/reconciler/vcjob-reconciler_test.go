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

package reconciler

import (
	"context"
	"errors"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/go-logr/logr"
	. "github.com/smartystreets/goconvey/convey"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	batch "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	scheduling "volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"github.com/raids-lab/crater/dao/model"
)

const (
	vcjobTestName      = "job-a"
	vcjobTestNamespace = "crater-workspace"
	vcjobTestUID       = "uid-1"
)

type failingGetClient struct {
	client.Client
	err error
}

func (c failingGetClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return c.err
}

func podGroupScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	utilruntime.Must(scheduling.AddToScheme(scheme))
	return scheme
}

func vcjobWithPhase(phase batch.JobPhase) *batch.Job {
	return &batch.Job{
		ObjectMeta: metav1.ObjectMeta{Name: vcjobTestName, Namespace: vcjobTestNamespace, UID: vcjobTestUID},
		Status:     batch.JobStatus{State: batch.JobState{Phase: phase}},
	}
}

func podGroupNamed(name string, phase scheduling.PodGroupPhase) *scheduling.PodGroup {
	return &scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: vcjobTestNamespace},
		Status:     scheduling.PodGroupStatus{Phase: phase},
	}
}

func reconcilerWith(objects ...client.Object) *VcJobReconciler {
	return &VcJobReconciler{
		Client: fake.NewClientBuilder().WithScheme(podGroupScheme()).WithObjects(objects...).Build(),
		log:    logr.Discard(),
	}
}

func TestPodGroupPhaseChanged(t *testing.T) {
	PatchConvey("only phase transitions pass the update filter", t, func() {
		pred := podGroupPhaseChanged()
		pending := podGroupNamed("pg", scheduling.PodGroupPending)
		inqueue := podGroupNamed("pg", scheduling.PodGroupInqueue)

		So(pred.Update(event.UpdateEvent{ObjectOld: pending, ObjectNew: inqueue}), ShouldBeTrue)
		So(pred.Update(event.UpdateEvent{ObjectOld: inqueue, ObjectNew: inqueue}), ShouldBeFalse)
		So(pred.Update(event.UpdateEvent{ObjectOld: &v1.Pod{}, ObjectNew: inqueue}), ShouldBeTrue)
		So(pred.Create(event.CreateEvent{Object: pending}), ShouldBeTrue)
		So(pred.Delete(event.DeleteEvent{Object: pending}), ShouldBeTrue)
	})
}

func TestIsActiveJobStatus(t *testing.T) {
	PatchConvey("counts pending, inqueue and running", t, func() {
		for _, phase := range []batch.JobPhase{batch.Pending, model.Inqueue, batch.Running} {
			So(isActiveJobStatus(phase), ShouldBeTrue)
		}
		for _, phase := range []batch.JobPhase{"", batch.Completed, batch.Failed, batch.Aborted, model.Prequeue} {
			So(isActiveJobStatus(phase), ShouldBeFalse)
		}
	})
}

func resolved(r *VcJobReconciler, t *testing.T, phase batch.JobPhase) []any {
	status, podGroupPhase, err := r.getJobStatus(t.Context(), vcjobWithPhase(phase))
	So(err, ShouldBeNil)
	return []any{status, podGroupPhase}
}

func TestResolveJobStatus(t *testing.T) {
	PatchConvey("non-pending phases pass through without a lookup", t, func() {
		r := &VcJobReconciler{log: logr.Discard()}
		So(resolved(r, t, batch.Running), ShouldResemble, []any{batch.Running, scheduling.PodGroupPhase("")})
		So(resolved(r, t, batch.Completed), ShouldResemble, []any{batch.Completed, scheduling.PodGroupPhase("")})
	})

	PatchConvey("pod group phase decides between pending and inqueue", t, func() {
		current := vcjobTestName + "-" + vcjobTestUID
		So(resolved(reconcilerWith(), t, ""), ShouldResemble, []any{batch.Pending, scheduling.PodGroupPhase("")})

		pending := reconcilerWith(podGroupNamed(current, scheduling.PodGroupPending))
		So(resolved(pending, t, batch.Pending), ShouldResemble, []any{batch.Pending, scheduling.PodGroupPending})

		for _, phase := range []scheduling.PodGroupPhase{
			scheduling.PodGroupInqueue, scheduling.PodGroupRunning, scheduling.PodGroupUnknown, scheduling.PodGroupCompleted,
		} {
			admitted := reconcilerWith(podGroupNamed(current, phase))
			So(resolved(admitted, t, batch.Pending), ShouldResemble, []any{model.Inqueue, phase})
		}

		legacy := reconcilerWith(podGroupNamed(vcjobTestName, scheduling.PodGroupRunning))
		So(resolved(legacy, t, ""), ShouldResemble, []any{model.Inqueue, scheduling.PodGroupRunning})
	})

	PatchConvey("update model carries the pod group phase next to the status", t, func() {
		current := vcjobTestName + "-" + vcjobTestUID
		r := reconcilerWith(podGroupNamed(current, scheduling.PodGroupRunning))

		starting, err := r.generateUpdateJobModel(t.Context(), vcjobWithPhase(batch.Pending), &model.Job{})
		So(err, ShouldBeNil)
		So(starting.Status, ShouldEqual, model.Inqueue)
		So(starting.PodGroupPhase, ShouldEqual, scheduling.PodGroupRunning)

		running, err := r.generateUpdateJobModel(t.Context(), vcjobWithPhase(batch.Running), &model.Job{})
		So(err, ShouldBeNil)
		So(running.Status, ShouldEqual, batch.Running)
		So(running.PodGroupPhase, ShouldBeEmpty)
	})

	PatchConvey("lookup failures propagate instead of demoting the job", t, func() {
		lookupErr := errors.New("cache not synced")
		r := &VcJobReconciler{
			Client: failingGetClient{Client: reconcilerWith().Client, err: lookupErr},
			log:    logr.Discard(),
		}
		group, err := r.getPodGroup(t.Context(), vcjobWithPhase(""))
		So(group, ShouldBeNil)
		So(errors.Is(err, lookupErr), ShouldBeTrue)

		// Reporting Pending here would drop an admitted job out of the quota ledger for good.
		status, podGroupPhase, resolveErr := r.getJobStatus(t.Context(), vcjobWithPhase(""))
		So(errors.Is(resolveErr, lookupErr), ShouldBeTrue)
		So(status, ShouldBeEmpty)
		So(podGroupPhase, ShouldBeEmpty)

		_, updateErr := r.generateUpdateJobModel(t.Context(), vcjobWithPhase(""), &model.Job{})
		So(errors.Is(updateErr, lookupErr), ShouldBeTrue)

		group, err = reconcilerWith().getPodGroup(t.Context(), vcjobWithPhase(""))
		So(group, ShouldBeNil)
		So(err, ShouldBeNil)
	})
}
