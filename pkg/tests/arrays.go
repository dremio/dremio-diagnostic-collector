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

package tests

func FindUniqueElements(array1, array2 []string) ([]string, []string) {
	uniqueToFirst := make(map[string]bool)
	uniqueToSecond := make(map[string]bool)

	// Iterate over the elements of the first array
	for _, str := range array1 {
		uniqueToFirst[str] = true
	}

	// Iterate over the elements of the second array
	for _, str := range array2 {
		if _, exists := uniqueToFirst[str]; exists {
			delete(uniqueToFirst, str)
		} else {
			uniqueToSecond[str] = true
		}
	}

	// Collect the elements unique to the first array
	uniqueFirst := make([]string, 0, len(uniqueToFirst))
	for str := range uniqueToFirst {
		uniqueFirst = append(uniqueFirst, str)
	}

	// Collect the elements unique to the second array
	uniqueSecond := make([]string, 0, len(uniqueToSecond))
	for str := range uniqueToSecond {
		uniqueSecond = append(uniqueSecond, str)
	}

	return uniqueFirst, uniqueSecond
}
