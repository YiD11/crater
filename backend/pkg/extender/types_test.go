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
	"encoding/json"
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestRequestJobInfo(t *testing.T) {
	t.Run("ownerJobName", func(t *testing.T) {
		PatchConvey("ownerJobName", t, func() {
			var nilReq *requestJobInfo
			So(nilReq.ownerJobName(), ShouldBeEmpty)
			So((&requestJobInfo{Name: jobA}).ownerJobName(), ShouldBeEmpty)
			So((&requestJobInfo{PodGroup: &requestPodGroup{}}).ownerJobName(), ShouldBeEmpty)

			req := vcjobRequest(jobA)
			So(req.ownerJobName(), ShouldEqual, jobA)

			req.PodGroup.OwnerReferences[0].Controller = ptr.To(false)
			So(req.ownerJobName(), ShouldBeEmpty)

			req = vcjobRequest(jobA)
			req.PodGroup.OwnerReferences = append([]metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "Deployment", Name: "not-a-job", Controller: ptr.To(true),
			}}, req.PodGroup.OwnerReferences...)
			So(req.ownerJobName(), ShouldEqual, jobA)
		})
	})

	t.Run("podGroupName", func(t *testing.T) {
		PatchConvey("podGroupName", t, func() {
			var nilReq *requestJobInfo
			So(nilReq.podGroupName(), ShouldBeEmpty)
			So((&requestJobInfo{Name: jobA}).podGroupName(), ShouldEqual, jobA)
			So((&requestJobInfo{Name: jobA, PodGroup: &requestPodGroup{}}).podGroupName(), ShouldEqual, jobA)
			So(vcjobRequest(jobA).podGroupName(), ShouldEqual, jobA+"-"+jobA+"-uid")
		})
	})

	t.Run("decodes volcano's wire format", func(t *testing.T) {
		PatchConvey("decodes volcano's wire format", t, func() {
			payload := `{"job":{"Name":"job-a","PodGroup":{"name":"job-a-uid","ownerReferences":[` +
				`{"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","name":"job-a","uid":"uid","controller":true}]}}}`
			var req jobEnqueueableRequest
			So(json.Unmarshal([]byte(payload), &req), ShouldBeNil)
			So(req.Job.ownerJobName(), ShouldEqual, jobA)
			So(req.Job.podGroupName(), ShouldEqual, "job-a-uid")

			So(json.Unmarshal([]byte(`{}`), &req), ShouldBeNil)
		})
	})
}
