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
	"errors"
	"testing"

	. "github.com/bytedance/mockey"
	"github.com/go-logr/logr"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/raids-lab/crater/dao/model"
)

func TestStart(t *testing.T) {
	t.Run("needs leader election", func(t *testing.T) {
		PatchConvey("needs leader election", t, func() {
			So((&PrequeueWatcher{}).NeedLeaderElection(), ShouldBeTrue)
		})
	})

	t.Run("swallows round errors and stops with the context", func(t *testing.T) {
		PatchConvey("swallows round errors and stops with the context", t, func() {
			round := Mock((*PrequeueWatcher).drainRound).Return(errors.New("db down")).Build()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			So((&PrequeueWatcher{logger: logr.Discard()}).Start(ctx), ShouldBeNil)
			So(round.MockTimes(), ShouldEqual, 1)
		})
	})
}

func TestDrainRound(t *testing.T) {
	w := &PrequeueWatcher{logger: logr.Discard()}

	t.Run("list failure ends the round", func(t *testing.T) {
		PatchConvey("list failure ends the round", t, func() {
			listErr := errors.New("db down")
			Mock((*PrequeueWatcher).listPrequeueJobs).Return(nil, listErr).Build()
			claim := Mock((*PrequeueWatcher).claimAndActivatePrequeueJob).Return(true, nil).Build()

			So(errors.Is(w.drainRound(t.Context()), listErr), ShouldBeTrue)
			So(claim.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("empty backlog submits nothing", func(t *testing.T) {
		PatchConvey("empty backlog submits nothing", t, func() {
			Mock((*PrequeueWatcher).listPrequeueJobs).Return([]*model.Job{}, nil).Build()
			claim := Mock((*PrequeueWatcher).claimAndActivatePrequeueJob).Return(true, nil).Build()

			So(w.drainRound(t.Context()), ShouldBeNil)
			So(claim.MockTimes(), ShouldEqual, 0)
		})
	})

	t.Run("one failure does not stop the batch", func(t *testing.T) {
		PatchConvey("one failure does not stop the batch", t, func() {
			seenLimit := 0
			Mock((*PrequeueWatcher).listPrequeueJobs).To(func(_ *PrequeueWatcher, _ context.Context, limit int) ([]*model.Job, error) {
				seenLimit = limit
				return []*model.Job{{JobName: "broken"}, {JobName: "raced"}, {JobName: "submitted"}}, nil
			}).Build()
			claim := Mock((*PrequeueWatcher).claimAndActivatePrequeueJob).To(
				func(_ *PrequeueWatcher, _ context.Context, candidate *model.Job) (bool, error) {
					switch candidate.JobName {
					case "broken":
						return false, errors.New("template broken")
					case "raced":
						return false, nil
					default:
						return true, nil
					}
				}).Build()

			So(w.drainRound(t.Context()), ShouldBeNil)
			So(claim.MockTimes(), ShouldEqual, 3)
			So(seenLimit, ShouldEqual, maxSubmitsPerRound)
		})
	})
}
