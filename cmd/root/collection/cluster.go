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

// collection module deals with specific k8s cluster level data collection
package collection

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

func ClusterK8sExecute(namespace string, cs CopyStrategy, ddfs helpers.Filesystem, c Collector, k string) error {
	cmds := []string{"nodes", "sc", "pvc", "pv", "service", "endpoints", "pods", "deployments", "statefulsets", "daemonset", "replicaset", "cronjob", "job", "events", "ingress", "limitrange", "resourcequota", "hpa", "pdb", "pc"}
	for _, cmd := range cmds {
		out, err := clusterExecuteBytes(namespace, cmd, c, k)
		if err != nil {
			simplelog.Errorf("when getting cluster config, error was %v", err)
			continue
		}
		text, err := masking.RemoveSecretsFromK8sJSON(string(out))
		if err != nil {
			simplelog.Errorf("unable to mask secrets for %v in namespace %v returning am empty text due to error '%v'", k, namespace, err)
			continue
		}
		p, err := cs.CreatePath("kubernetes", "dremio-master", "")
		if err != nil {
			simplelog.Errorf("trying to construct cluster config path %v with error %v", p, err)
			continue
		}
		path := strings.TrimSuffix(p, "dremio-master")
		filename := filepath.Join(path, cmd+".json")
		err = ddfs.WriteFile(filename, []byte(text), DirPerms)
		if err != nil {
			simplelog.Errorf("trying to write file %v, error was %v", filename, err)
			continue
		}
	}
	p, err := cs.CreatePath("kubernetes", "container-logs", "")
	if err != nil {
		simplelog.Errorf("trying to construct cluster config path %v with error %v", p, err)
		return err
	}

	// We can reuse the FindHosts command to grab all dremio pos in our namespace
	pods, err := c.FindHosts("role=dremio-cluster-pod")
	if err != nil {
		simplelog.Errorf("trying to find pods with error %v", err)
		return err
	}
	// Loop over dremio pods
	for _, pod := range pods {
		kubectlArgs := []string{k, "-n", namespace, "get", "pods", string(pod), "-o", `jsonpath={.spec['containers','initContainers'][*].name}`}
		containers, err := clusterExecutePod(kubectlArgs)
		if err != nil {
			simplelog.Errorf("trying to list containers from pod %v with error %v", pod, err)
			return err
		}
		// Loop over each container, construct a path and log file name
		// write the output of the kubectl logs command to a file
		for _, container := range s2s(containers) {
			kubectlArgs = []string{k, "-n", namespace, "logs", pod, "-c", string(container)}
			out, err := clusterExecutePod(kubectlArgs)
			if err != nil {
				simplelog.Errorf("trying to get logs failed with error: %v", err)
				continue
			}
			outFile := filepath.Join(p, pod+"-"+container+".out")
			simplelog.Infof("getting logs for pod: %v container: %v", pod, container)
			p, err := cs.CreatePath("kubernetes", "container-logs", "")
			if err != nil {
				simplelog.Errorf("trying to create container log path \n%v \nwith error \n%v", p, err)
				continue
			}
			// Write out the logs to a file
			err = ddfs.WriteFile(outFile, []byte(out), DirPerms)
			if err != nil {
				simplelog.Errorf("trying to write file %v, error was %v", outFile, err)
				continue
			}
		}
	}
	return nil
}

// Execute commands at the cluster level
// Calls a raw execute function and simply writes out the byte array read from the response
// that comes in directly from kubectl
func clusterExecuteBytes(namespace, cmd string, _ Collector, k string) ([]byte, error) {
	cli := &cli.Cli{}
	kubectlArgs := []string{k, "-n", namespace, "get", cmd}
	kubectlArgs = append(kubectlArgs, "-o", "json")
	simplelog.Infof("cluster command: %v", kubectlArgs)
	res, err := cli.ExecuteBytes(kubectlArgs...)
	if err != nil {
		return []byte(""), fmt.Errorf("when getting config %v error returned was %v", cmd, err)
	}
	return res, nil
}

// Execute commands at the cluster level
// Returns response as a string (instead of bytes)
func clusterExecutePod(args []string) (string, error) {
	cli := &cli.Cli{}
	res, err := cli.Execute(args...)
	if err != nil {
		return "", fmt.Errorf("when running command \n%v\nerror returned was %v", args, err)
	}
	return res, nil
}

// Convert string to a []string
func s2s(s string) []string {
	return (strings.Split(s, " "))
}
