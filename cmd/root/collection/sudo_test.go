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

package collection_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/collection"
)

func TestComposeExecuteAndStreamWithSudo(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}
	var outputCaptured []string
	output := func(line string) {
		outputCaptured = append(outputCaptured, line)
	}
	command := []string{"ls", "-la", "/"}
	if err := collection.ComposeExecuteAndStream(false, conf, output, command); err != nil {
		t.Fatal(err)
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"sudo", "-u", "serviceUser", "ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeExecuteAndStreamWithoutSudo(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}
	var outputCaptured []string
	output := func(line string) {
		outputCaptured = append(outputCaptured, line)
	}
	command := []string{"ls", "-la", "/"}
	if err := collection.ComposeExecuteAndStream(false, conf, output, command); err != nil {
		t.Fatal(err)
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeExecuteAndStreamWithSudoWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}
	var outputCaptured []string
	output := func(line string) {
		outputCaptured = append(outputCaptured, line)
	}
	command := []string{"ls", "-la", "/"}
	if err := collection.ComposeExecuteAndStream(false, conf, output, command); err == nil {
		t.Fatal(err)
	} else {
		if err.Error() != "silly error 1" {
			t.Errorf("expected %v but got %v", "silly error 1", err)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"sudo", "-u", "serviceUser", "ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeExecuteAndStreamWithoutSudoWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}
	var outputCaptured []string
	output := func(line string) {
		outputCaptured = append(outputCaptured, line)
	}
	command := []string{"ls", "-la", "/"}
	if err := collection.ComposeExecuteAndStream(false, conf, output, command); err == nil {
		t.Fatal(err)
	} else {
		if err.Error() != "silly error 1" {
			t.Errorf("expected %v but got %v", "silly error 1", err)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeExecuteWithSudo(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}
	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecute(false, conf, command); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expectedArray := []string{"sudo", "-u", "serviceUser", "ls", "-la", "/"}
	if !reflect.DeepEqual(args, expectedArray) {
		t.Errorf("expected %#v but got %#v", expectedArray, args)
	}
}

func TestComposeExecuteWithoutSudo(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecute(false, conf, command); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expectedArray := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expectedArray) {
		t.Errorf("expected %#v but got %#v", expectedArray, args)
	}
}

func TestComposeExecuteWithSudoWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{"", fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecute(false, conf, command); err == nil {
		t.Fatal(err)
	} else {
		if !strings.Contains(err.Error(), "silly error 1") {
			t.Errorf("expected '%v' but got '%v'", "silly error 1", err)
		}
		if txt != "" {
			t.Errorf("expected nil but got %v", txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"sudo", "-u", "serviceUser", "ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeExecuteWithoutSudoWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{"", fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecute(false, conf, command); err == nil {
		t.Fatal(err)
	} else {
		if !strings.Contains(err.Error(), "silly error 1") {
			t.Errorf("expected %v but got %v", "silly error 1", err)
		}
		if txt != "" {
			t.Errorf("expected nil but got %v", txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeNoSudoExecute(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecuteNoSudo(false, conf, command); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expectedArray := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expectedArray) {
		t.Errorf("expected %#v but got %#v", expectedArray, args)
	}
}

func TestComposeNoSudoExecuteWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{"", fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecuteNoSudo(false, conf, command); err == nil {
		t.Fatal(err)
	} else {
		if err.Error() != "silly error 1" {
			t.Errorf("expected %v but got %v", "silly error 1", err)
		}
		if txt != "" {
			t.Errorf("expected nil but got %v", txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}

func TestComposeCopy(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	source := "abc"
	dest := "def"
	if txt, err := collection.ComposeCopy(conf, source, dest); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	hostString := firstCall["hostString"]
	if hostString != "myHost" {
		t.Errorf("expected myHost but got %v", hostString)
	}
	isCoord := firstCall["isCoordinator"]
	if isCoord != true {
		t.Errorf("expected true but got %v", isCoord)
	}
	foundSource := firstCall["source"]
	if foundSource != source {
		t.Errorf("expected %v but was %v", source, foundSource)
	}
	foundDest := firstCall["destination"]
	if foundDest != dest {
		t.Errorf("expected %v but was %v", dest, foundDest)
	}
	call := firstCall["call"]
	if call != "copyFromHost" {
		t.Errorf("expected to call copyFromHost but called %v", call)
	}
}

func TestComposeCopyWithSudo(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}

	source := "abc"
	dest := "def"
	if txt, err := collection.ComposeCopy(conf, source, dest); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	hostString := firstCall["hostString"]
	if hostString != "myHost" {
		t.Errorf("expected myHost but got %v", hostString)
	}
	isCoord := firstCall["isCoordinator"]
	if isCoord != true {
		t.Errorf("expected true but got %v", isCoord)
	}
	foundSource := firstCall["source"]
	if foundSource != source {
		t.Errorf("expected %v but was %v", source, foundSource)
	}
	foundDest := firstCall["destination"]
	if foundDest != dest {
		t.Errorf("expected %v but was %v", dest, foundDest)
	}
	call := firstCall["call"]
	if call != "copyFromHostSudo" {
		t.Errorf("expected to call copyFromHostSudo but called %v", call)
	}
}
func TestComposeCopyTo(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	source := "abc"
	dest := "def"
	if txt, err := collection.ComposeCopyTo(conf, source, dest); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	hostString := firstCall["hostString"]
	if hostString != "myHost" {
		t.Errorf("expected myHost but got %v", hostString)
	}
	isCoord := firstCall["isCoordinator"]
	if isCoord != true {
		t.Errorf("expected true but got %v", isCoord)
	}
	foundSource := firstCall["source"]
	if foundSource != source {
		t.Errorf("expected %v but was %v", source, foundSource)
	}
	foundDest := firstCall["destination"]
	if foundDest != dest {
		t.Errorf("expected %v but was %v", dest, foundDest)
	}
	call := firstCall["call"]
	if call != "copyToHost" {
		t.Errorf("expected to call copyToHost but called %v", call)
	}
}

func TestComposeCopyToWithSudo(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "serviceUser",
	}

	source := "abc"
	dest := "def"
	if txt, err := collection.ComposeCopyTo(conf, source, dest); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	hostString := firstCall["hostString"]
	if hostString != "myHost" {
		t.Errorf("expected myHost but got %v", hostString)
	}
	isCoord := firstCall["isCoordinator"]
	if isCoord != true {
		t.Errorf("expected true but got %v", isCoord)
	}
	foundSource := firstCall["source"]
	if foundSource != source {
		t.Errorf("expected %v but was %v", source, foundSource)
	}
	foundDest := firstCall["destination"]
	if foundDest != dest {
		t.Errorf("expected %v but was %v", dest, foundDest)
	}
	call := firstCall["call"]
	if call != "copyToHostSudo" {
		t.Errorf("expected to call copyToHostSudo but called %v", call)
	}
}

func TestComposeNoSudoCopy(t *testing.T) {
	expected := "works!"
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{expected, nil}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	source := "abc"
	dest := "def"
	if txt, err := collection.ComposeCopyNoSudo(conf, source, dest); err != nil {
		t.Fatal(err)
	} else {
		if txt != expected {
			t.Errorf("expected %v but got %v", expected, txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	hostString := firstCall["hostString"]
	if hostString != "myHost" {
		t.Errorf("expected myHost but got %v", hostString)
	}
	isCoord := firstCall["isCoordinator"]
	if isCoord != true {
		t.Errorf("expected true but got %v", isCoord)
	}
	foundSource := firstCall["source"]
	if foundSource != source {
		t.Errorf("expected %v but was %v", source, foundSource)
	}
	foundDest := firstCall["destination"]
	if foundDest != dest {
		t.Errorf("expected %v but was %v", dest, foundDest)
	}
	call := firstCall["call"]
	if call != "copyFromHost" {
		t.Errorf("expected to call copyFromHost but called %v", call)
	}
}

func TestComposeNoSudoCopyWithError(t *testing.T) {
	mockCollect := &collection.MockCollector{
		Returns: [][]interface{}{{"", fmt.Errorf("silly error %v", 1)}},
	}
	conf := collection.HostCaptureConfiguration{
		Host:          "myHost",
		Collector:     mockCollect,
		IsCoordinator: true,
		SudoUser:      "",
	}

	command := []string{"ls", "-la", "/"}
	if txt, err := collection.ComposeExecuteNoSudo(false, conf, command); err == nil {
		t.Fatal(err)
	} else {
		if err.Error() != "silly error 1" {
			t.Errorf("expected %v but got %v", "silly error 1", err)
		}
		if txt != "" {
			t.Errorf("expected nil but got %v", txt)
		}
	}
	firstCall := mockCollect.Calls[0]
	args := firstCall["args"]
	expected := []string{"ls", "-la", "/"}
	if !reflect.DeepEqual(args, expected) {
		t.Errorf("expected %#v but got %#v", expected, args)
	}
}
