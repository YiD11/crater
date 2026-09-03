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

package utils

import (
	"testing"

	. "github.com/bytedance/mockey"
	. "github.com/smartystreets/goconvey/convey"
	"k8s.io/apimachinery/pkg/util/sets"
)

func TestNodeConstraintsOverlap(t *testing.T) {
	t.Run("empty constraints are wildcards", func(t *testing.T) {
		PatchConvey("empty constraints are wildcards", t, func() {
			So(NodeConstraintsOverlap(nil, nil), ShouldBeTrue)
			So(NodeConstraintsOverlap(sets.New[string](), sets.New("node-1")), ShouldBeTrue)
			So(NodeConstraintsOverlap(sets.New("node-1"), nil), ShouldBeTrue)
			So(NodeConstraintsOverlap(sets.New("node-1"), sets.New("node-2")), ShouldBeFalse)
			So(NodeConstraintsOverlap(sets.New("node-1", "node-2"), sets.New("node-2", "node-3")), ShouldBeTrue)
		})
	})
}
