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

// kubernetes package provides access to log collections on k8s
package kubernetes

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

// CommandExecutor is an interface that represents the functionality of executing commands.
type CommandExecutor interface {
	ExecuteCommand(command string, args ...string) ([]byte, error)
}

// DefaultCommandExecutor is the default implementation of the CommandExecutor interface.
type DefaultCommandExecutor struct{}

// ExecuteCommand executes the specified command and returns the output and any error.
func (e *DefaultCommandExecutor) ExecuteCommand(command string, args ...string) ([]byte, error) {
	cmd := exec.Command(command, args...)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() failed with %v\n", err)
		return nil, err
	}
	return out.Bytes(), nil
}

func GetK8sLogs(executor CommandExecutor, namespace, outputDir string) error {
	simplelog.Infof("Getting list of pods")
	err, pods := GetPods(executor, namespace)
	if err != nil {
		return fmt.Errorf("Error getting pods %v", err)
	}
	for _, pod := range pods {
		simplelog.Infof("Getting list of containers for %v", pod)
		err = GetContainerLogs(executor, pod, outputDir)
		if err != nil {
			return fmt.Errorf("Error getting containers %v", err)

		}
		simplelog.Infof("Getting list of init containers for %v", pod)
		err = GetInitContainerLogs(executor, pod, outputDir)
		if err != nil {
			return fmt.Errorf("Error getting init containers %v", err)

		}
	}
	return nil

}

func GetPods(executor CommandExecutor, namespace string) (err error, pods []string) {

	getPodsCmd := fmt.Sprintf("kubectl get pods -n %v -o name", namespace)
	out, err := executor.ExecuteCommand("bash", "-c", getPodsCmd)
	//var outBuff bytes.Buffer
	//var stderr bytes.Buffer
	//cmd.Stdout = &out
	//cmd.Stderr = &stderr
	//err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() failed with %v\n", err)
	}
	// Convert []byte to bytes.Buffer
	outputBuffer := bytes.NewBuffer(out)

	// Convert bytes.Buffer to string
	podNames := outputBuffer.String()
	simplelog.Debugf("Pods found: %v", podNames)
	pods = split(podNames, "\n")

	return err, pods
}

func GetInitContainerLogs(executor CommandExecutor, podName, outputDir string) (err error) {
	pod := strings.TrimPrefix(podName, "pod/")
	getContainerCmd := fmt.Sprintf("kubectl get pod %v -o jsonpath=\"{.spec['containers','initContainers'][*].name}\"", pod)
	out, err := executor.ExecuteCommand("bash", "-c", getContainerCmd)
	//cmd := exec.Command("bash", "-c", getContainerCmd)
	//var out bytes.Buffer
	//var stderr bytes.Buffer
	//cmd.Stdout = &out
	//cmd.Stderr = &stderr
	//err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() %v failed with %v\n", getContainerCmd, err)
	}
	// Convert []byte to bytes.Buffer
	outputBuffer := bytes.NewBuffer(out)

	// Convert bytes.Buffer to string
	containerNames := outputBuffer.String()
	containers := split(containerNames, " ")
	for _, container := range containers {
		outFile := filepath.Join(outputDir, pod+"-"+container+".out")
		simplelog.Infof("creating file: %v", outFile)
		cLog, err := os.Create(outFile)
		if err != nil {
			fmt.Println("Error creating file:", err)
			return err
		}
		defer cLog.Close()
		getLogsCmd := fmt.Sprintf("kubectl logs %v -c %v", pod, container)
		logCmd := exec.Command("bash", "-c", getLogsCmd)
		var logs bytes.Buffer
		var logsStderr bytes.Buffer
		logCmd.Stdout = &logs
		logCmd.Stderr = &logsStderr
		errLogs := logCmd.Run()
		if errLogs != nil {
			log.Printf("cmd.Run() failed with %v\n", errLogs)
		}
		simplelog.Infof("Logs for container %v in pod %v", container, pod)
		// Write the contents of the buffer to the file
		_, err = logs.WriteTo(cLog)
		if err != nil {
			fmt.Println("Error writing to file:", err)
			return err
		}
	}
	return err
}

func GetContainerLogs(executor CommandExecutor, podName, outputDir string) (err error) {
	pod := strings.TrimPrefix(podName, "pod/")
	getContainerCmd := fmt.Sprintf("kubectl get pod %v -o jsonpath=\"{.spec['containers','initContainers'][*].name}\"", pod)
	out, err := executor.ExecuteCommand("bash", "-c", getContainerCmd)
	//cmd := exec.Command("bash", "-c", getContainerCmd)
	//var out bytes.Buffer
	//var stderr bytes.Buffer
	//cmd.Stdout = &out
	//cmd.Stderr = &stderr
	//err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() %v failed with %v\n", getContainerCmd, err)
	}
	// Convert []byte to bytes.Buffer
	outputBuffer := bytes.NewBuffer(out)

	// Convert bytes.Buffer to string
	containerNames := outputBuffer.String()
	containers := split(containerNames, " ")
	for _, container := range containers {
		outFile := filepath.Join(outputDir, pod+"-"+container+".out")
		simplelog.Infof("creating file: %v", outFile)
		cLog, err := os.Create(outFile)
		if err != nil {
			fmt.Println("Error creating file:", err)
			return err
		}
		defer cLog.Close()
		getLogsCmd := fmt.Sprintf("kubectl logs %v -c %v", pod, container)
		logCmd := exec.Command("bash", "-c", getLogsCmd)
		var logs bytes.Buffer
		var logsStderr bytes.Buffer
		logCmd.Stdout = &logs
		logCmd.Stderr = &logsStderr
		errLogs := logCmd.Run()
		if errLogs != nil {
			log.Printf("cmd.Run() failed with %v\n", errLogs)
		}
		simplelog.Infof("Logs for container %v in pod %v", container, pod)
		// Write the contents of the buffer to the file
		_, err = logs.WriteTo(cLog)
		if err != nil {
			fmt.Println("Error writing to file:", err)
			return err
		}
	}
	return err
}

func split(s, sep string) (result []string) {
	a := strings.Split(s, sep)
	for _, str := range a {
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}
