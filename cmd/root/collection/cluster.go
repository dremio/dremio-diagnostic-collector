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
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func ClusterK8sExecute(namespace string, cs CopyStrategy, ddfs helpers.Filesystem, k *kubernetes.KubectlK8sActions) error {
	cmds := []string{"nodes", "sc", "pvc", "pv", "service", "endpoints", "pods", "deployments", "statefulsets", "daemonset", "replicaset", "cronjob", "job", "events", "ingress", "limitrange", "resourcequota", "hpa", "pdb", "pc"}
	var wg sync.WaitGroup
	p, err := cs.CreatePath("kubernetes", "dremio-master", "")
	if err != nil {
		simplelog.Errorf("trying to construct cluster config path %v with error %v", p, err)
		return err
	}
	for _, cmd := range cmds {
		wg.Add(1)
		go func(cmdname string) {
			defer wg.Done()
			resource := cmdname
			out, err := k.GetClient().RESTClient().Get().Resource(resource).Do(context.TODO()).Raw()
			if err != nil {
				simplelog.Errorf("when getting cluster config, error was %v", err)
				return
			}
			text, err := masking.RemoveSecretsFromK8sJSON(string(out))
			if err != nil {
				simplelog.Errorf("unable to mask secrets for %v in namespace %v returning am empty text due to error '%v'", k, namespace, err)
				return
			}

			path := strings.TrimSuffix(p, "dremio-master")
			filename := filepath.Join(path, resource+".json")
			err = ddfs.WriteFile(filename, []byte(text), DirPerms)
			if err != nil {
				simplelog.Errorf("trying to write file %v, error was %v", filename, err)
				return
			}
			consoleprint.UpdateK8sFiles(cmdname)
		}(cmd)
	}
	wg.Wait()
	return nil
}

func GetClusterLogs(namespace string, cs CopyStrategy, ddfs helpers.Filesystem, k *kubernetes.KubectlK8sActions, pods []string) error {
	var wg sync.WaitGroup
	path, err := cs.CreatePath("kubernetes", "container-logs", "")
	if err != nil {
		simplelog.Errorf("trying to construct cluster container log path %v with error %v", path, err)
		return err
	}

	// Loop over dremio pods
	for _, pod := range pods {
		wg.Add(1)
		go func(podname string) {
			defer wg.Done()
			//kubectlArgs := []string{k, "-n", namespace, "get", "pods", string(podname), "-o", `jsonpath={.spec['containers','initContainers'][*].name}`}
			podObj, err := k.GetClient().CoreV1().Pods(namespace).Get(context.Background(), podname, meta_v1.GetOptions{})
			if err != nil {
				simplelog.Errorf("trying to list containers from pod %v with error %v", podname, err)
				return
			}
			var containers []string
			for _, c := range podObj.Spec.Containers {
				containers = append(containers, c.Name)
			}
			for _, c := range podObj.Spec.InitContainers {
				containers = append(containers, c.Name)
			}
			//Loop over each container, construct a path and log file name
			//write the output of the kubectl logs command to a file
			for _, container := range containers {
				copyContainerLog(cs, ddfs, k, container, namespace, path, podname)
			}
			consoleprint.UpdateK8sFiles(fmt.Sprintf("pod %v logs", podname))
		}(pod)
	}
	wg.Wait()
	return err
}

func copyContainerLog(cs CopyStrategy, ddfs helpers.Filesystem, k *kubernetes.KubectlK8sActions, container, namespace, path, pod string) {
	out, err := k.GetClient().CoreV1().Pods(namespace).GetLogs(pod, &v1.PodLogOptions{}).Do(context.Background()).Raw()
	if err != nil {
		simplelog.Errorf("trying to get log from pod: %v container: %v with error: %v", pod, container, err)
	}
	outFile := filepath.Join(path, pod+"-"+container+".txt")
	simplelog.Debugf("getting logs for pod: %v container: %v", pod, container)
	p, err := cs.CreatePath("kubernetes", "container-logs", "")
	if err != nil {
		simplelog.Errorf("trying to create container log path \n%v \nwith error \n%v", p, err)
	}
	// Write out the logs to a file
	err = ddfs.WriteFile(outFile, []byte(out), DirPerms)
	if err != nil {
		simplelog.Errorf("trying to write file %v, error was %v", outFile, err)
	}
}

// func GetClusterNodes(namespace string, cs CopyStrategy, ddfs helpers.Filesystem, k string) error {
// 	path, err := cs.CreatePath("kubernetes", "nodes", "")
// 	if err != nil {
// 		simplelog.Errorf("trying to construct cluster node path %v with error %v", path, err)
// 		return err
// 	}

// 	kubectlArgs := []string{k, "-n", namespace, "describe", "nodes"}
// 	nodes, err := clusterExecutePod(kubectlArgs)
// 	if err != nil {
// 		simplelog.Errorf("trying to list nodes from cluster with error %v", err)
// 		return err
// 	}
// 	// // Write the output of the kubectl describe nodes command to a file
// 	// outFile := filepath.Join(path, "describe-nodes.txt")
// 	// err = ddfs.WriteFile(outFile, []byte(nodes), DirPerms)
// 	// if err != nil {
// 	// 	simplelog.Errorf("trying to write file %v, error was %v", outFile, err)
// 	// }
// 	consoleprint.UpdateK8sFiles("cluster describe nodes")

// 	return err
// }

// func GetClusterPods(namespace string, cs CopyStrategy, ddfs helpers.Filesystem, k *kubernetes.KubectlK8sActions) error {
// 	k.GetClient()
// 	path, err := cs.CreatePath("kubernetes", "pods", "")
// 	if err != nil {
// 		simplelog.Errorf("trying to construct cluster pods path %v with error %v", path, err)
// 		return err
// 	}

// 	// kubectlArgs := []string{k, "-n", namespace, "describe", "pods"}
// 	// nodes, err := clusterExecutePod(kubectlArgs)
// 	// if err != nil {
// 	// 	simplelog.Errorf("trying to describe pods from cluster with error %v", err)
// 	// 	return err
// 	// }
// 	// Write the output of the kubectl describe nodes command to a file
// 	outFile := filepath.Join(path, "describe-pods.txt")
// 	err = ddfs.WriteFile(outFile, []byte(nodes), DirPerms)
// 	if err != nil {
// 		simplelog.Errorf("trying to write file %v, error was %v", outFile, err)
// 	}
// 	consoleprint.UpdateK8sFiles("cluster describe pods")

// 	return err
// }

// // Execute commands at the cluster level
// // Calls a raw execute function and simply writes out the byte array read from the response
// // that comes in directly from kubectl
// func clusterExecuteBytes(namespace, resource string, _ Collector, k string) ([]byte, error) {
// 	cli := &cli.Cli{}
// 	kubectlArgs := []string{k, "-n", namespace, "get", resource}
// 	kubectlArgs = append(kubectlArgs, "-o", "json")
// 	res, err := cli.ExecuteBytes(false, kubectlArgs...)
// 	if err != nil {
// 		return []byte(""), fmt.Errorf("when getting config %v error returned was %v", resource, err)
// 	}
// 	return res, nil
// }

// // Execute commands at the cluster level
// // Returns response as a string (instead of bytes)
// func clusterExecutePod(args []string) (string, error) {
// 	cli := &cli.Cli{}
// 	res, err := cli.Execute(false, args...)
// 	if err != nil {
// 		return "", fmt.Errorf("when running command \n%v\nerror returned was %v", args, err)
// 	}
// 	return res, nil
// }
