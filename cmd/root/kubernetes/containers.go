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

func GetK8sLogs(namespace, outputDir string) error {
	err, pods := getPods(namespace)
	if err != nil {
		return fmt.Errorf("Error getting pods %v", err)
	}
	for _, pod := range pods {
		err = getContainerLogs(pod, outputDir)
		if err != nil {
			return fmt.Errorf("Error getting containers %v", err)

		}
		err = GetInitContainerLogs(pod, outputDir)
		if err != nil {
			return fmt.Errorf("Error getting init containers %v", err)

		}
	}
	return nil

}

func getPods(namespace string) (err error, pods []string) {

	getPodsCmd := fmt.Sprintf("kubectl get pods -n %s -o name", namespace)
	cmd := exec.Command("bash", "-c", getPodsCmd)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() failed with %s\n", err)
	}
	podNames := out.String()
	pods = split(podNames, " ")

	return err, pods
}

func GetInitContainerLogs(podName, outputDir string) (err error) {

	getContainerCmd := fmt.Sprintf("kubectl get pod %s -o jsonpath=\"{.spec['containers','initContainers'][*].name}\"", podName)
	cmd := exec.Command("bash", "-c", getContainerCmd)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() failed with %s\n", err)
	}
	containerNames := out.String()
	containers := split(containerNames, " ")
	for _, container := range containers {
		cLog, err := os.Create(filepath.Join(outputDir, podName+"-"+container))
		if err != nil {
			fmt.Println("Error creating file:", err)
			return err
		}
		defer cLog.Close()
		getLogsCmd := fmt.Sprintf("kubectl logs %s -c %s", podName, container)
		logCmd := exec.Command("bash", "-c", getLogsCmd)
		var logs bytes.Buffer
		var logsStderr bytes.Buffer
		logCmd.Stdout = &logs
		logCmd.Stderr = &logsStderr
		errLogs := logCmd.Run()
		if errLogs != nil {
			log.Printf("cmd.Run() failed with %s\n", errLogs)
		}
		fmt.Println("Logs for container", container, "in pod", podName, ":")
		// Write the contents of the buffer to the file
		_, err = logs.WriteTo(cLog)
		if err != nil {
			fmt.Println("Error writing to file:", err)
			return err
		}
	}
	return err
}

func getContainerLogs(podName, outputDir string) (err error) {

	getContainerCmd := fmt.Sprintf("kubectl get pod %s -o jsonpath=\"{.spec['containers'][*].name}\"", podName)
	cmd := exec.Command("bash", "-c", getContainerCmd)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		simplelog.Errorf("cmd.Run() failed with %s\n", err)
	}
	containerNames := out.String()
	containers := split(containerNames, " ")
	for _, container := range containers {
		cLog, err := os.Create(filepath.Join(outputDir, podName+"-"+container))
		if err != nil {
			fmt.Println("Error creating file:", err)
			return err
		}
		defer cLog.Close()
		getLogsCmd := fmt.Sprintf("kubectl logs %s -c %s", podName, container)
		logCmd := exec.Command("bash", "-c", getLogsCmd)
		var logs bytes.Buffer
		var logsStderr bytes.Buffer
		logCmd.Stdout = &logs
		logCmd.Stderr = &logsStderr
		errLogs := logCmd.Run()
		if errLogs != nil {
			log.Printf("cmd.Run() failed with %s\n", errLogs)
		}
		fmt.Println("Logs for container", container, "in pod", podName, ":")
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
