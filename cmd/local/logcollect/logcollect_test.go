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

package logcollect_test

import (
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/local/ddcio"
	"github.com/dremio/dremio-diagnostic-collector/cmd/local/logcollect"
	"github.com/dremio/dremio-diagnostic-collector/pkg/tests"
)

func cleanUp(dirs ...string) {
	for _, d := range dirs {
		log.Printf("deleting %v", d)
		if err := ddcio.DeleteDirContents(d); err != nil {
			log.Printf("WARN: unable to delete the contents of folder %v due to error %v", d, err)
		}
	}
}

var logCollector logcollect.Collector
var destinationQueriesJSON string
var startLogDir string
var testGCLogsDir string
var dremioLogDays int
var dremioQueriesJSONDays int

var setupEnv = func() (destinationDir, logDir string) {
	dremioLogDays = 2
	dremioQueriesJSONDays = 4
	startLogDir = filepath.Join("testdata", "logDirWithAllLogs")
	testGCLogsDir = filepath.Join("testdata", "gcLogDir")
	destinationQueriesJSON = filepath.Join("testdata", "queriesOutDir")
	var err error
	err = os.MkdirAll(destinationQueriesJSON, 0750)
	if err != nil {
		log.Fatalf("unexpected error %v", err)
	}
	logDir, err = os.MkdirTemp("", "SOURCE*")
	if err != nil {
		log.Fatalf("unexpected error %v", err)
	}
	destinationDir, err = os.MkdirTemp("", "DESTINATION*")
	if err != nil {
		log.Fatalf("unexpected error %v", err)
	}
	logCollector = *logcollect.NewLogCollector(
		logDir,
		destinationDir,
		testGCLogsDir,
		"gc.*.log*",
		destinationQueriesJSON,
		dremioQueriesJSONDays,
		dremioLogDays,
	)
	return destinationDir, logDir
}

var AfterEachLogCollectTest = func() {
	if err := ddcio.DeleteDirContents(destinationQueriesJSON); err != nil {
		log.Printf("unable to delete the contents of folder %v due to error %v", destinationQueriesJSON, err)
	}
}

func TestLogCollect_WhenAllServerLogsArePresent(t *testing.T) {
	var destinationDir string
	var testLogDir string
	var yesterdaysLog string
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()
	//It("should collect all logs", func() {
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	//rename archive to yesterday
	yesterdaysLog = "server." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "server.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	tests.Tree(testLogDir)
	err := logCollector.RunCollectDremioServerLog()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	tests.Tree(destinationDir)
	actual := filepath.Join(destinationDir, "server.log.gz")
	expected := filepath.Join(testLogDir, "server.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, "server.out")
	expected = filepath.Join(testLogDir, "server.out")
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenServerLogHasAnUngzippedFileInTheArchive(t *testing.T) {
	var destinationDir string
	var testLogDir string
	var yesterdaysLog string
	var dayBeforeYesterday string
	var err error
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	//rename archive to yesterday
	yesterdaysLog = "server." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "server.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	//copy server.log to day before yesterday
	dayBeforeYesterday = "server." + time.Now().AddDate(0, 0, -2).Format("2006-01-02") + ".log"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "server.log"), filepath.Join(testLogDir, "archive", dayBeforeYesterday)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioServerLog()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should should collect the server.log as a gzip", func() {
	actual := filepath.Join(destinationDir, "server.log.gz")
	expected := filepath.Join(testLogDir, "server.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	//It("should collect server.out", func() {
	actual = filepath.Join(destinationDir, "server.out")
	expected = filepath.Join(testLogDir, "server.out")
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

	//It("should find the gzipped file and copy it as is", func() {
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
	//It("should find the ungzipped file and archive it", func() {
	actual = filepath.Join(destinationDir, dayBeforeYesterday+".gz")
	expected = filepath.Join(testLogDir, "archive", dayBeforeYesterday)
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenServerOutIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	//rename archive to yesterday
	yesterdaysLog = "server." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "server.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "server.out")); err != nil {
		t.Logf("test should fail as we had an error removing the server.out: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioServerLog()
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	if err == nil {
		t.Error("expected an error but there was none")
	}

	//It("should collect all logs with age", func() {
	actual := filepath.Join(destinationDir, "server.log.gz")
	expected := filepath.Join(testLogDir, "server.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

	//It("should ignore logs older than num days", func() {
	_, err = os.Stat(filepath.Join(destinationDir, "server.2022-04-30.log.gz"))
	if !os.IsNotExist(err) && err == nil {
		t.Error("should not copy the file but did")
	}
}

func TestLogCollect_WhenServerLogIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "server." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "server.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	if err := os.Remove(filepath.Join(testLogDir, "server.log")); err != nil {
		t.Logf("test should fail as we had an error removing the server.log: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioServerLog()

	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	if err == nil {
		t.Error("expected an error but there was none")
	}

	//It("should collect all logs still present", func() {
	actual := filepath.Join(destinationDir, "server.out")
	expected := filepath.Join(testLogDir, "server.out")
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

}
func TestLogCollect_WhenServerLogArchivesAreMissing(t *testing.T) {
	var err error
	var destinationDir string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioServerLog()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect all logs still present", func() {
	actual := filepath.Join(destinationDir, "server.log.gz")
	expected := filepath.Join(testLogDir, "server.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, "server.out")
	expected = filepath.Join(testLogDir, "server.out")
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

}

func TestLogCollect_WhenAllReflectionLogsArePresent(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "reflection." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "reflection.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		treeOut := tests.TreeToString(filepath.Join(testLogDir, "archive"))
		t.Logf("test should fail as we had an error setting up the test directory: %v with dir of %v", err, treeOut)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectReflectionLogs()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect all logs", func() {
	actual := filepath.Join(destinationDir, "reflection.log.gz")
	expected := filepath.Join(testLogDir, "reflection.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

	//it should not collect logs older than num log days
	_, err = os.Stat(filepath.Join(destinationDir, "reflection.2022-04-30.log.gz"))
	if !os.IsNotExist(err) && err == nil {
		t.Error("should not copy the file but did")
	}
}
func TestLogCollect_WhenReflectionLogArchivesAreMissing(t *testing.T) {
	var err error
	var destinationDir string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectReflectionLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect reflection.log", func() {
	actual := filepath.Join(destinationDir, "reflection.log.gz")
	expected := filepath.Join(testLogDir, "reflection.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenReflectionLogIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "reflection.log")); err != nil {
		t.Logf("test should fail as we had an error removing the reflection.log: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	yesterdaysLog = "reflection." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "reflection.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectReflectionLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect archives", func() {
	actual := filepath.Join(destinationDir, yesterdaysLog)
	expected := filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenAllAccelerationLogsArePresent(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "acceleration." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "acceleration.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectAccelerationLogs()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect all logs", func() {
	actual := filepath.Join(destinationDir, "acceleration.log.gz")
	expected := filepath.Join(testLogDir, "acceleration.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

	//It("should ignore logs older than num days", func() {
	_, err = os.Stat(filepath.Join(destinationDir, "acceleration.2022-04-30.log.gz"))
	if !os.IsNotExist(err) && err == nil {
		t.Error("should not copy the file but did")
	}
}
func TestLogCollect_WhenAccelerationLogArchivesAreMissing(t *testing.T) {
	var err error
	var destinationDir string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectAccelerationLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect acceleration log as a gzip", func() {
	actual := filepath.Join(destinationDir, "acceleration.log.gz")
	expected := filepath.Join(testLogDir, "acceleration.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenAccelerationLogIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "acceleration.log")); err != nil {
		t.Logf("test should fail as we had an error removing the acceleration.log: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	yesterdaysLog = "acceleration." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "acceleration.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectAccelerationLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect archives", func() {
	actual := filepath.Join(destinationDir, yesterdaysLog)
	expected := filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenAllAcessLogsArePresent(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "access." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "access.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioAccessLogs()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect all logs", func() {
	actual := filepath.Join(destinationDir, "access.log.gz")
	expected := filepath.Join(testLogDir, "access.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}

	//It("should ignore logs older than num days", func() {
	_, err = os.Stat(filepath.Join(destinationDir, "access.2022-04-30.log.gz"))
	if !os.IsNotExist(err) && err == nil {
		t.Error("should not copy the file but did")
	}
}
func TestLogCollect_WhenAccessLogArchiveAreMissing(t *testing.T) {
	var err error
	var destinationDir string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioAccessLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect access log as a gzip", func() {
	actual := filepath.Join(destinationDir, "access.log.gz")
	expected := filepath.Join(testLogDir, "access.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenAaccessLogIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "access.log")); err != nil {
		t.Logf("test should fail as we had an error removing the access.log: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	yesterdaysLog = "access." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "access.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectDremioAccessLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect archives", func() {
	actual := filepath.Join(destinationDir, yesterdaysLog)
	expected := filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenAllMetadataRefreshLogsArePresent(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "metadata_refresh." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "metadata_refresh.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectMetadataRefreshLogs()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect all logs", func() {
	actual := filepath.Join(destinationDir, "metadata_refresh.log.gz")
	expected := filepath.Join(testLogDir, "metadata_refresh.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationDir, yesterdaysLog)
	expected = filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
	//It("should ignore logs older than num days", func() {
	_, err = os.Stat(filepath.Join(destinationDir, "metadata_refresh.2022-04-30.log.gz"))
	if !os.IsNotExist(err) {
		t.Errorf("expected to not find old log file copied but it was")
	}
}

func TestLogCollect_WhenMetadataRefreshLogArchivesAreMissing(t *testing.T) {
	var err error
	var destinationDir string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectMetadataRefreshLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect access log as a gzip", func() {
	actual := filepath.Join(destinationDir, "metadata_refresh.log.gz")
	expected := filepath.Join(testLogDir, "metadata_refresh.log")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenMetadataRefreshLogIsMissing(t *testing.T) {
	var err error
	var destinationDir string
	var yesterdaysLog string
	var testLogDir string
	destinationDir, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "metadata_refresh.log")); err != nil {
		t.Logf("test should fail as we had an error removing the metadata_refresh.log: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	yesterdaysLog = "metadata_refresh." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".log.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "metadata_refresh.2022-04-30.log.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectMetadataRefreshLogs()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationDir, testLogDir)
	defer AfterEachLogCollectTest()

	//	It("should collect archives", func() {
	actual := filepath.Join(destinationDir, yesterdaysLog)
	expected := filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenAllQueriesJsonLogsArePresent(t *testing.T) {
	var err error
	var yesterdaysLog string
	var testLogDir string
	_, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	yesterdaysLog = "queries." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".json.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "queries.2022-04-30.json.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	tests.Tree(testLogDir)
	err = logCollector.RunCollectQueriesJSON()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationQueriesJSON, testLogDir)
	defer AfterEachLogCollectTest()
	tests.Tree(destinationQueriesJSON)

	//It("should collect all logs", func() {
	actual := filepath.Join(destinationQueriesJSON, "queries.json.gz")
	expected := filepath.Join(testLogDir, "queries.json")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
	actual = filepath.Join(destinationQueriesJSON, yesterdaysLog)
	expected = filepath.Join(destinationQueriesJSON, yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
	//It("should ignore logs older than num days", func() {
	_, err = os.Stat(filepath.Join(destinationQueriesJSON, "queries.2022-04-30.json.gz"))
	if !os.IsNotExist(err) {
		t.Errorf("expected to not find old log file copied but it was")
	}
}

func TestLogCollect_WhenQueriesJsonArchivesAreMissing(t *testing.T) {
	var err error
	var testLogDir string
	_, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}
	// just deleting the archive folder entirely
	if err := os.RemoveAll(filepath.Join(testLogDir, "archive")); err != nil {
		t.Errorf("unexpected error %v", err)
	}
	err = logCollector.RunCollectQueriesJSON()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationQueriesJSON, testLogDir)
	defer AfterEachLogCollectTest()

	//It("should collect access log as a gzip", func() {
	tests.Tree(destinationQueriesJSON)
	actual := filepath.Join(destinationQueriesJSON, "queries.json.gz")
	expected := filepath.Join(testLogDir, "queries.json")
	if match, err := tests.ContainThisFileInTheGzip(expected, actual); !match && err != nil {
		t.Errorf("expected %v file to contain %v but it did not", expected, actual)
	}
}

func TestLogCollect_WhenQueriesJsonIsMissing(t *testing.T) {
	var err error
	var yesterdaysLog string
	var testLogDir string
	_, testLogDir = setupEnv()
	//setup logs
	if err := ddcio.CopyDir(startLogDir, testLogDir); err != nil {
		t.Logf("test should fail as we had an error setting up the test directory: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	if err := os.Remove(filepath.Join(testLogDir, "queries.json")); err != nil {
		t.Logf("ERROR test should fail as we had an error removing the queries.json: %v", err)
		t.Errorf("unexpected error %v", err)
	}

	yesterdaysLog = "queries." + time.Now().AddDate(0, 0, -1).Format("2006-01-02") + ".json.gz"
	if err := ddcio.CopyFile(filepath.Join(testLogDir, "archive", "queries.2022-04-30.json.gz"), filepath.Join(testLogDir, "archive", yesterdaysLog)); err != nil {
		t.Logf("ERROR test should fail as we had an error setting up the test directory: %v", err)
		if err != nil {
			t.Errorf("unexpected error %v", err)
		}
	}
	err = logCollector.RunCollectQueriesJSON()
	if err == nil {
		t.Error("expected an error but there was none")
	}
	defer cleanUp(destinationQueriesJSON, testLogDir)
	defer AfterEachLogCollectTest()
	tests.Tree(destinationQueriesJSON)

	//It("should collect queriesJSON archives", func() {
	actual := filepath.Join(destinationQueriesJSON, yesterdaysLog)
	expected := filepath.Join(testLogDir, "archive", yesterdaysLog)
	if match, err := tests.MatchFile(expected, actual); !match && err != nil {
		t.Errorf("expected %v file content does not match file content of %v", expected, actual)
	}
}

func TestLogCollect_WhenGCLogsArePresentAndSomeHaveModTimeMoreThanLogDays(t *testing.T) {
	destinationDir, _ := setupEnv()
	gcLogFromToday := "gc.0.log"
	currentTime := time.Now()

	err := os.Chtimes(filepath.Join(testGCLogsDir, gcLogFromToday), currentTime, currentTime)
	if err != nil {
		t.Fatal(err)
	}
	gcLogFromYesterday := "gc.1.log"
	yesterday := currentTime.AddDate(0, 0, -1)
	err = os.Chtimes(filepath.Join(testGCLogsDir, gcLogFromYesterday), yesterday, yesterday)
	if err != nil {
		t.Fatal(err)
	}
	gcLogFromLastWeek := "gc.2.log"
	lastWeek := currentTime.AddDate(0, 0, -7)
	err = os.Chtimes(filepath.Join(testGCLogsDir, gcLogFromLastWeek), lastWeek, lastWeek)
	if err != nil {
		t.Fatal(err)
	}

	err = logCollector.RunCollectGcLogs()
	if err != nil {
		t.Errorf("unexpected error %v", err)
	}
	defer cleanUp(destinationDir)
	defer AfterEachLogCollectTest()
	assertFileCopied := func(gclog string) {
		actual := filepath.Join(destinationDir, gclog)
		expected := filepath.Join(testGCLogsDir, gclog)
		if match, err := tests.MatchFile(expected, actual); !match && err != nil {
			t.Errorf("expected %v file content does not match file content of %v, error report was :%v", expected, actual, err)
		}
	}

	assertFileCopied(gcLogFromToday)
	assertFileCopied(gcLogFromYesterday)

	_, err = os.Stat(filepath.Join(destinationDir, gcLogFromLastWeek))
	// we are assuming an error also means not copied even though in theory it could
	// we should still fail the test as we don't have a clear ideal of the state of the system
	fileNotCopied := err != nil
	if !fileNotCopied {
		t.Errorf("was expecting file %v to not be copied but it was despite being too old", gcLogFromLastWeek)
	}
}

func TestLogCollect_WhenGCLogsArePresentAndThereAreMoreThanOne(t *testing.T) {
	var destinationDir string
	//It("should collect all gc logs as gzips", func() {
	tests.Tree(destinationDir)
	tests.Tree(testGCLogsDir)
	gclogs := []string{"gc.0.log", "gc.1.log", "gc.2.log", "gc.3.log", "gc.4.log.current"}
	for _, gclog := range gclogs {
		t.Run("Test GCLog "+gclog, func(t *testing.T) {
			destinationDir, _ = setupEnv()
			//update mod time of each
			expected := filepath.Join(testGCLogsDir, gclog)
			currentTime := time.Now()
			// Change both the access time and the modification time to current time
			err := os.Chtimes(expected, currentTime, currentTime)
			if err != nil {
				log.Fatal(err)
			}
			err = logCollector.RunCollectGcLogs()
			if err != nil {
				t.Errorf("unexpected error %v", err)
			}
			defer cleanUp(destinationDir)
			defer AfterEachLogCollectTest()
			actual := filepath.Join(destinationDir, gclog)
			if match, err := tests.MatchFile(expected, actual); !match && err != nil {
				t.Errorf("expected %v file content does not match file content of %v", expected, actual)
			}
		})
	}
}
