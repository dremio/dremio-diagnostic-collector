//	Copyright 2023 Dremio Corporation
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

// package validation concerns itself with validation configuration
package validation

import (
	"fmt"

	"github.com/dremio/dremio-diagnostic-collector/pkg/collects"
)

func ValidateCollectMode(collectionMode string) error {
	if collectionMode != collects.HealthCheckCollection && collectionMode != collects.QuickCollection && collectionMode != collects.StandardCollection {
		return fmt.Errorf("invalid --collect option '%v' the only valid options are %v, %v, and %v", collectionMode, collects.QuickCollection, collects.StandardCollection, collects.HealthCheckCollection)
	}
	return nil
}
