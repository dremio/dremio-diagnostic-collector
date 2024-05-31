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

// kubectl package provides access to log collections on k8s
package kubectl

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
)

type KubeArgs struct {
	Namespace string
}

// NewKubectlK8sActions is the only supported way to initialize the KubectlK8sActions struct
// one must pass the path to kubectl
func NewKubectlK8sActions(hook *shutdown.Hook, namespace string) (*CliK8sActions, error) {
	_, err := exec.LookPath("kubectl")
	if err != nil {
		return &CliK8sActions{}, fmt.Errorf("no kubectl found: %v", err)
	}
	return &CliK8sActions{
		cli:         cli.NewCli(hook),
		kubectlPath: "kubectl",
		namespace:   namespace,
		pidHosts:    make(map[string]string),
	}, nil
}

// CliK8sActions provides a way to collect and copy files using kubectl
type CliK8sActions struct {
	cli         cli.CmdExecutor
	kubectlPath string
	namespace   string
	pidHosts    map[string]string
}

func (c *CliK8sActions) cleanLocal(rawDest string) string {
	//windows does the wrong thing for kubectl here and provides a path with C:\ we need to remove it as kubectl detects this as a remote destination
	return strings.TrimPrefix(rawDest, "C:")
}

func (c *CliK8sActions) getContainerName(podName string) (string, error) {
	conts, err := c.cli.Execute(false, c.kubectlPath, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[0].name}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(conts), nil
}

func (c *CliK8sActions) Name() string {
	return "Kubectl"
}

func (c *CliK8sActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) (err error) {
	container, err := c.getContainerName(hostString)
	if err != nil {
		return fmt.Errorf("unable to get container name: %v", err)
	}
	kubectlArgs := []string{c.kubectlPath, "exec", "-n", c.namespace, "-c", container, hostString, "--"}
	kubectlArgs = append(kubectlArgs, args...)
	return c.cli.ExecuteAndStreamOutput(mask, output, pat, kubectlArgs...)
}

func (c *CliK8sActions) HostExecute(mask bool, hostString string, args ...string) (out string, err error) {
	var outBuilder strings.Builder
	writer := func(line string) {
		outBuilder.WriteString(line)
	}
	err = c.HostExecuteAndStream(mask, hostString, writer, "", args...)
	out = outBuilder.String()
	return
}

func (c *CliK8sActions) CopyFromHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	container, err := c.getContainerName(hostString)
	if err != nil {
		return "", fmt.Errorf("unable to get container name: %v", err)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", container, "--retries", "25", fmt.Sprintf("%v:%v", hostString, source), c.cleanLocal(destination))
}

func (c *CliK8sActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	container, err := c.getContainerName(hostString)
	if err != nil {
		return "", fmt.Errorf("unable to get container name: %v", err)
	}
	return c.cli.Execute(false, c.kubectlPath, "cp", "-n", c.namespace, "-c", container, "--retries", "50", c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
}

func (c *CliK8sActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *CliK8sActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
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
func (c *CliK8sActions) GetExecutors() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return container == "dremio-executor"
	})
}

func (c *CliK8sActions) HelpText() string {
	return "Make sure the labels and namespace you use actually correspond to your dremio pods: try something like 'ddc -n mynamespace --coordinator app=dremio-coordinator --executor app=dremio-executor'.  You can also run 'kubectl get pods --show-labels' to see what labels are available to use for your dremio pods"
}

func (c *CliK8sActions) SetHostPid(host, pidFile string) {
	c.pidHosts[host] = pidFile
}

func (c *CliK8sActions) CleanupRemote() error {
	kill := func(host string, pidFile string) {
		if pidFile == "" {
			simplelog.Debugf("pidfile is blank for %v skipping", host)
		}
		container, err := c.getContainerName(host)
		if err != nil {
			simplelog.Warningf("output of container for host %v: %v", host, err)
			return
		}
		kubectlArgs := []string{"exec", "-n", c.namespace, "-c", container, host, "--"}
		kubectlArgs = append(kubectlArgs, "cat")
		kubectlArgs = append(kubectlArgs, pidFile)
		ctx, timeoutPid := context.WithTimeout(context.Background(), time.Second*time.Duration(30))
		defer timeoutPid()
		simplelog.Infof("getting pid for host %v", host)
		cmd := exec.CommandContext(ctx, c.kubectlPath, kubectlArgs...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			simplelog.Warningf("output of pidfile failed for host %v: %v -%v", host, err, string(out[:]))
			return
		}
		simplelog.Infof("pid for host %v is %v", host, string(out[:]))
		kubectlArgs = []string{"exec", "-n", c.namespace, "-c", container, host, "--"}
		kubectlArgs = append(kubectlArgs, "kill")
		kubectlArgs = append(kubectlArgs, "-15")
		kubectlArgs = append(kubectlArgs, string(out[:]))
		ctx, timeoutKill := context.WithTimeout(context.Background(), time.Second*time.Duration(120))
		defer timeoutKill()
		cmd = exec.CommandContext(ctx, c.kubectlPath, kubectlArgs...)
		killOut, err := cmd.CombinedOutput()
		if err != nil {
			simplelog.Warningf("failed killing process %v host %v: %v -%v", out, host, err, string(killOut[:]))
			return
		}
		consoleprint.UpdateNodeState(consoleprint.NodeState{
			Node:     host,
			Status:   consoleprint.Starting,
			StatusUX: "FAILED - CANCELLED",
			Result:   consoleprint.ResultFailure,
		})
		//cancel out so we can skip if it's called again
		c.pidHosts[host] = ""
	}
	var criticalErrors []string
	coordinators, err := c.GetCoordinators()
	if err != nil {
		msg := fmt.Sprintf("unable to get coordinators for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, coordinator := range coordinators {
			if v, ok := c.pidHosts[coordinator]; ok {
				kill(coordinator, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", coordinator)
			}
		}
	}

	executors, err := c.GetExecutors()
	if err != nil {
		msg := fmt.Sprintf("unable to get executors for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, executor := range executors {
			if v, ok := c.pidHosts[executor]; ok {
				kill(executor, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", executor)
			}
		}
	}
	if len(criticalErrors) > 0 {
		return fmt.Errorf("critical errors trying to cleanup pods %v", strings.Join(criticalErrors, ", "))
	}
	return nil
}
