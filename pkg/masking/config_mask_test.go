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

package masking_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
)

func TestConfig_WhenRemoveSecretsFromDremioConfAndConfIsNotFound(t *testing.T) {
	//It("should throw an error if the provided file is not a dremio.conf", func() {
	err := masking.RemoveSecretsFromDremioConf("testdata/myFile.txt")
	if err == nil {
		t.Errorf("we expected an error but there was not one")
	}
	if !strings.Contains(err.Error(), "expected file with name 'dremio.conf', got ") {
		t.Errorf("should be 'expected file with name 'dremio.conf'' in string '%v' but there was not", err.Error())
	}
}

func TestConfig_WhenRemoveSecretsFromDremioConf(t *testing.T) {
	//It("should mask secrets in the config file", func() {
	// We'll write the dremio.conf contents to a temporary file for this test
	conf := `
			paths: {
				local: ${DREMIO_HOME}"/data"
				dist: "pdfs://"${paths.local}"/pdfs"
			}
	
			services: {
				executor: {
					cache: {
						path.db: "/opt/dremio/cloudcache/c0"
						pctquota.db: 100
	
						path.fs: ["/opt/dremio/cloudcache/c0"]
						pctquota.fs: [100]
						ensurefreespace.fs: [0]
	
					}
				}
	
				javax.net.ssl {
					keyStore: "",
					keyStorePassword: "oh silly man",
					trustStore:"",
					trustStorePassword: "why do you do this"
				}
			}
			`
	tmpDir, err := os.MkdirTemp("", "ddctester")
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	tmpfile := filepath.Join(tmpDir, "dremio.conf")
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("cannot remove test dir %v due to error %v", tmpDir, err)
		}
	}()

	err = os.WriteFile(tmpfile, []byte(conf), 0700)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	err = masking.RemoveSecretsFromDremioConf(tmpfile)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	cleanedConf, err := os.ReadFile(tmpfile)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	//should mask passwords in dremio.conf
	if !strings.Contains(string(cleanedConf), `keyStorePassword: "<REMOVED_POTENTIAL_SECRET>"`) {
		t.Errorf("keystorePassword was not masked")
	}
	if !strings.Contains(string(cleanedConf), `trustStorePassword: "<REMOVED_POTENTIAL_SECRET>"`) {
		t.Errorf("trustStorePassword was not masked")
	}
}

func TestPATMask(t *testing.T) {
	token := `pLnvVgLjQNKg0BMm+qpZe4xJVP0l7At8I7iuMu26lyZ5gx9YxF7KffTIIBbVQw==`
	cmd := `ddc -k -c app=dremio-executor -e app=dremio-executor --dremio-pat-token ` + token
	expected := `ddc -k -c app=dremio-executor -e app=dremio-executor "<REMOVED_PAT_TOKEN>"`
	returned := masking.MaskPAT(cmd)
	if strings.Compare(returned, expected) != 0 {
		t.Errorf("\nexpected %v\nreturned %v\n", expected, returned)
	}
	cmd = `ddc -k -c app=dremio-executor -e app=dremio-executor -t ` + token
	returned = masking.MaskPAT(cmd)
	if strings.Compare(returned, expected) != 0 {
		t.Errorf("\nexpected %v\nreturned %v\n", expected, returned)
	}
}
