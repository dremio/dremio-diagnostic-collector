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
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type KubeArgs struct {
	Namespace   string
	KubectlPath string
}

// NewKubectlK8sActions is the only supported way to initialize the KubectlK8sActions struct
// one must pass the path to kubectl
func NewKubectlK8sActions(kubeArgs KubeArgs) *KubectlK8sActions {
	return &KubectlK8sActions{
		cli:         &cli.Cli{},
		kubectlPath: kubeArgs.KubectlPath,
		namespace:   kubeArgs.Namespace,
	}
}

// KubectlK8sActions provides a way to collect and copy files using kubectl
type KubectlK8sActions struct {
	cli         cli.CmdExecutor
	kubectlPath string
	namespace   string
}

func (c *KubectlK8sActions) cleanLocal(rawDest string) string {
	//windows does the wrong thing for kubectl here and provides a path with C:\ we need to remove it as kubectl detects this as a remote destination
	return strings.TrimPrefix(rawDest, "C:")
}

func (c *KubectlK8sActions) getContainerName(podName string) (string, error) {
	conts, err := c.cli.Execute(false, c.kubectlPath, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[0].name}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(conts), nil
}

func (c *KubectlK8sActions) Name() string {
	return "Kubectl"
}

func (c *KubectlK8sActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, args ...string) (err error) {
	container, err := c.getContainerName(hostString)
	if err != nil {
		return fmt.Errorf("unable to get container name: %v", err)
	}
	kubectlArgs := []string{c.kubectlPath, "exec", "-n", c.namespace, "-c", container, hostString, "--"}
	kubectlArgs = append(kubectlArgs, args...)
	return c.cli.ExecuteAndStreamOutput(mask, output, kubectlArgs...)
}

func (c *KubectlK8sActions) HostExecute(mask bool, hostString string, args ...string) (out string, err error) {
	var outBuilder strings.Builder
	writer := func(line string) {
		outBuilder.WriteString(line)
	}
	err = c.HostExecuteAndStream(mask, hostString, writer, args...)
	out = outBuilder.String()
	return
}

func (c *KubectlK8sActions) CopyFromHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	container, err := c.getContainerName(hostString)
	if err != nil {
		return "", fmt.Errorf("unable to get container name: %v", err)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", container, fmt.Sprintf("%v:%v", hostString, source), c.cleanLocal(destination))
}

func (c *KubectlK8sActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	container, err := c.getContainerName(hostString)
	if err != nil {
		return "", fmt.Errorf("unable to get container name: %v", err)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", container, c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
}

func (c *KubectlK8sActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *KubectlK8sActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
	out, err := c.cli.Execute(false, c.kubectlPath, "get", "pods", "-n", c.namespace, "-l", "role=dremio-cluster-pod", "-o", "name")
	if err != nil {
		return []string{}, err
	}
	rawPods := strings.Split(out, "\n")
	var pods []string
	var lock sync.RWMutex
	var wg sync.WaitGroup
	for _, pod := range rawPods {
		podCopy := pod
		if podCopy == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			rawPod := strings.TrimSpace(podCopy)
			podCopy := rawPod[4:]
			container, err := c.getContainerName(podCopy)
			if err != nil {
				simplelog.Errorf("unable to get pod name (%v): %v", podCopy, err)
				return
			}
			if compare(container) {
				lock.Lock()
				pods = append(pods, podCopy)
				lock.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Strings(pods)
	return pods, nil
}
func (c *KubectlK8sActions) GetExecutors() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return container == "dremio-executor"
	})
}

func (c *KubectlK8sActions) HelpText() string {
	return "Make sure the labels and namespace you use actually correspond to your dremio pods: try something like 'ddc -n mynamespace --coordinator app=dremio-coordinator --executor app=dremio-executor'.  You can also run 'kubectl get pods --show-labels' to see what labels are available to use for your dremio pods"
}

func GetClusters(kubectl string) ([]string, error) {
	c := &cli.Cli{}
	out, err := c.Execute(false, kubectl, "get", "ns", "-o", "name")
	if err != nil {
		return []string{}, err
	}
	var namespaces []string
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		namespaces = append(namespaces, strings.TrimPrefix(l, "namespace/"))
	}
	var dremioClusters []string
	var wg sync.WaitGroup
	var lock sync.RWMutex
	for _, n := range namespaces {
		nCopy := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := c.Execute(false, kubectl, "get", "pods", "-n", nCopy, "-l", "role=dremio-cluster-pod", "-o", "name")
			if err != nil {
				simplelog.Errorf("unable find pods in namespace %v: %v", err, nCopy)
			}
			if len(strings.Split(strings.TrimSpace(out), "\n")) > 0 {
				lock.Lock()
				dremioClusters = append(dremioClusters, nCopy)
				lock.Unlock()
			}
		}()

	}
	wg.Wait()
	return dremioClusters, nil
}
