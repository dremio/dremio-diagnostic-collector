// Copyright 2023 Dremio Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package collection

import (
	"errors"
	"runtime"
	"sync"

	"github.com/crgimenes/native/power"

	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

const keepAwakeReason = "Dremio Diagnostic Collector collection"

// keepAwake blocks idle system sleep and returns the function that lifts the
// block. It is a variable so tests can replace it.
var keepAwake = func(reason string) (release func() error, err error) {
	tok, err := power.PreventSleep(reason)
	if err != nil {
		return nil, err
	}
	return tok.Release, nil
}

// startKeepAwake blocks idle system sleep for the duration of a collection.
// The returned stop function is never nil and is safe to call more than once.
func startKeepAwake() (stop func()) {
	release, err := keepAwake(keepAwakeReason)
	if err != nil {
		if errors.Is(err, power.ErrUnsupported) {
			simplelog.Infof("keep-awake: not supported on %s", runtime.GOOS)
		} else {
			simplelog.Warningf("keep-awake: unavailable: %v", err)
		}
		return func() {}
	}
	simplelog.Info("keep-awake: idle system sleep blocked until the collection ends")
	var once sync.Once
	return func() {
		once.Do(func() {
			if err := release(); err != nil {
				simplelog.Warningf("keep-awake: release failed: %v", err)
				return
			}
			simplelog.Info("keep-awake: released")
		})
	}
}
