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

// apicollect provides all the methods that collect via the API, this is a substantial part of the activities of DDC so it gets it's own package
package apicollect

import (
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/local/conf"
)

var (
	testConf *conf.CollectConf
)

func TestSysTableNameWithNoEscapableCharacters(t *testing.T) {
	urlsuffix := ""

	name := getSystemTableName("thing", urlsuffix)
	expected := "sys.thing.json"
	if name != expected {
		t.Errorf("expected %v but was %v", expected, name)
	}
}

func TestSysTableNameWithBackslashAndDoubleQuotes(t *testing.T) {
	urlsuffix := ""

	name := getSystemTableName("\\\"thing\\\"", urlsuffix)
	expected := "sys.thing.json"
	if name != expected {
		t.Errorf("expected %v but was %v", expected, name)
	}
}

func TestSysTableNameWithQuestionMarkAndEqualsCharacters(t *testing.T) {
	urlsuffix := "?offset=0"

	name := getSystemTableName("thing", urlsuffix)
	expected := "sys.thing_offset_0.json"
	if name != expected {
		t.Errorf("expected %v but was %v", expected, name)
	}
}

func TestSysTableNameWithAllEscapableCharacters(t *testing.T) {
	urlsuffix := "?offset=0&limit=500"

	name := getSystemTableName("\\\"tables\\\"", urlsuffix)
	expected := "sys.tables_offset_0_limit_500.json"
	if name != expected {
		t.Errorf("expected %v but was %v", expected, name)
	}
}

func TestSysTableSkip(t *testing.T) {
	var c conf.CollectConf
	table := c.SkipSysTables()
	t.Logf("table: %v", table)

}
