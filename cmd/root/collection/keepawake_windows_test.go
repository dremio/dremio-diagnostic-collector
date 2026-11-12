//go:build windows

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
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	systemExecutionState = 16 // POWER_INFORMATION_LEVEL SystemExecutionState
	esSystemRequired     = 0x1
)

func readExecutionState(t *testing.T) uint32 {
	t.Helper()
	proc := windows.NewLazySystemDLL("powrprof.dll").NewProc("CallNtPowerInformation")
	var state uint32
	r, _, _ := proc.Call(systemExecutionState, 0, 0, uintptr(unsafe.Pointer(&state)), unsafe.Sizeof(state))
	if r != 0 {
		t.Fatalf("CallNtPowerInformation status = %#x", r)
	}
	return state
}

func TestKeepAwake_RealWindowsSetsSystemRequired(t *testing.T) {
	alreadyHeld := readExecutionState(t)&esSystemRequired != 0

	stop := startKeepAwake()
	released := false
	defer func() {
		if !released {
			stop()
		}
	}()
	if readExecutionState(t)&esSystemRequired == 0 {
		t.Error("ES_SYSTEM_REQUIRED not set while keep-awake is held")
	}

	stop()
	released = true
	if alreadyHeld {
		t.Skip("ES_SYSTEM_REQUIRED was already set by another process; cannot check release")
	}
	if readExecutionState(t)&esSystemRequired != 0 {
		t.Error("ES_SYSTEM_REQUIRED still set after release")
	}
}
