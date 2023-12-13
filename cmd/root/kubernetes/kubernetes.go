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
	"strings"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
)

type KubeArgs struct {
	Namespace            string
	CoordinatorContainer string
	ExecutorsContainer   string
	KubectlPath          string
}

// NewKubectlK8sActions is the only supported way to initialize the KubectlK8sActions struct
// one must pass the path to kubectl
func NewKubectlK8sActions(kubeArgs KubeArgs) *KubectlK8sActions {
	return &KubectlK8sActions{
		cli:                  &cli.Cli{},
		kubectlPath:          kubeArgs.KubectlPath,
		coordinatorContainer: kubeArgs.CoordinatorContainer,
		executorContainer:    kubeArgs.ExecutorsContainer,
		namespace:            kubeArgs.Namespace,
	}
}

// KubectlK8sActions provides a way to collect and copy files using kubectl
type KubectlK8sActions struct {
	cli                  cli.CmdExecutor
	kubectlPath          string
	coordinatorContainer string
	executorContainer    string
	namespace            string
}

func (c *KubectlK8sActions) cleanLocal(rawDest string) string {
	//windows does the wrong thing for kubectl here and provides a path with C:\ we need to remove it as kubectl detects this as a remote destination
	return strings.TrimPrefix(rawDest, "C:")
}

func (c *KubectlK8sActions) getContainerName(podName string, isCoordinator bool) string {
	if isCoordinator {
		kubectlArgs := []string{c.kubectlPath, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec['containers','initContainers'][*].name}`}
		conts, _ := c.cli.Execute(false, kubectlArgs...)
		containers := strings.Split(conts, " ")
		expectedContainers := strings.Split(c.coordinatorContainer, ",")
		for _, container := range containers {
			for _, expectedContainer := range expectedContainers {
				if container == expectedContainer {
					return container
				}
			}

		}
	}
	// All other pod types are executors
	return c.executorContainer
}

func (c *KubectlK8sActions) Name() string {
	return "Kubectl"
}

func (c *KubectlK8sActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, isCoordinator bool, args ...string) (err error) {
	kubectlArgs := []string{c.kubectlPath, "exec", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), hostString, "--"}
	kubectlArgs = append(kubectlArgs, args...)
	return c.cli.ExecuteAndStreamOutput(mask, output, kubectlArgs...)
}

func (c *KubectlK8sActions) HostExecute(mask bool, hostString string, isCoordinator bool, args ...string) (out string, err error) {
	kubectlArgs := []string{c.kubectlPath, "exec", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), hostString, "--"}
	kubectlArgs = append(kubectlArgs, args...)
	return c.cli.Execute(mask, kubectlArgs...)
}

func (c *KubectlK8sActions) CopyFromHost(hostString string, isCoordinator bool, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), fmt.Sprintf("%v:%v", hostString, source), c.cleanLocal(destination))
}

func (c *KubectlK8sActions) CopyFromHostSudo(hostString string, isCoordinator bool, _, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	// We dont have any sudo user in the container so no addition of sudo commands used
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), fmt.Sprintf("%v:%v", hostString, source), c.cleanLocal(destination))
}

func (c *KubectlK8sActions) CopyToHost(hostString string, isCoordinator bool, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
}

func (c *KubectlK8sActions) CopyToHostSudo(hostString string, isCoordinator bool, _, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	// We dont have any sudo user in the container so no addition of sudo commands used
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", c.getContainerName(hostString, isCoordinator), c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
}

func (c *KubectlK8sActions) FindHosts(searchTerm string) (podName []string, err error) {
	out, err := c.cli.Execute(false, c.kubectlPath, "get", "pods", "-n", c.namespace, "-l", searchTerm, "-o", "name")
	if err != nil {
		return []string{}, err
	}
	rawPods := strings.Split(out, "\n")
	var pods []string
	for _, pod := range rawPods {
		if pod == "" {
			continue
		}
		rawPod := strings.TrimSpace(pod)
		//log.Print(rawPod)
		pod := rawPod[4:]
		//log.Print(pod)
		pods = append(pods, pod)
	}
	return pods, nil
}

func (c *KubectlK8sActions) HelpText() string {
	return "Make sure the labels and namespace you use actually correspond to your dremio pods: try something like 'ddc -n mynamespace --coordinator app=dremio-coordinator --executor app=dremio-executor'.  You can also run 'kubectl get pods --show-labels' to see what labels are available to use for your dremio pods"
}
