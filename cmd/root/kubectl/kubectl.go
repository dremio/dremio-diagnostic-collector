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
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
)

type KubeArgs struct {
	Namespace string
}

// NewKubectlK8sActions is the only supported way to initialize the KubectlK8sActions struct
// one must pass the path to kubectl
func NewKubectlK8sActions(hook shutdown.CancelHook, kubeArgs kubernetes.KubeArgs) (*CliK8sActions, error) {
	kubectl, err := exec.LookPath("kubectl")
	if err != nil {
		return &CliK8sActions{}, fmt.Errorf("no kubectl found: %w", err)
	}
	cliInstance := cli.NewCli(hook)
	k8sContext := kubeArgs.K8SContext
	if k8sContext == "" {
		ctxArgs := []string{kubectl}
		if kubeArgs.KubeconfigPath != "" {
			ctxArgs = append(ctxArgs, "--kubeconfig", kubeArgs.KubeconfigPath)
		}
		ctxArgs = append(ctxArgs, "config", "current-context")
		k8sContextRaw, err := cliInstance.Execute(false, ctxArgs...)
		if err != nil {
			return &CliK8sActions{}, fmt.Errorf("unable to retrieve context: %w", err)
		}
		k8sContext = strings.TrimSpace(k8sContextRaw)
	}
	retriesEnabled, err := CanRetryTransfers(kubectl)
	if err != nil {
		return &CliK8sActions{}, fmt.Errorf("unable to run kubectl version so disabling kubectl: %w", err)
	}
	return &CliK8sActions{
		cli:                 cliInstance,
		kubectlPath:         kubectl,
		detectLabelSelector: kubeArgs.DetectLabelSelector,
		namespace:           kubeArgs.Namespace,
		k8sContext:          k8sContext,
		kubeconfigPath:      kubeArgs.KubeconfigPath,
		pidHosts:            make(map[string]string),
		containerNames:      make(map[string]string),
		retriesEnabled:      retriesEnabled,
	}, nil
}

// CliK8sActions provides a way to collect and copy files using kubectl
type CliK8sActions struct {
	cli                 cli.CmdExecutor
	detectLabelSelector string
	kubectlPath         string
	namespace           string
	k8sContext          string
	kubeconfigPath      string
	pidHosts            map[string]string
	containerNames      map[string]string
	m                   sync.Mutex
	retriesEnabled      bool
}

// k8sFlags returns the cluster-routing flags (--kubeconfig, --context)
// that must precede any kubectl subcommand. Empty values are omitted.
// Order matches kubectl convention: global flags before subcommand.
func (c *CliK8sActions) k8sFlags() []string {
	var flags []string
	if c.kubeconfigPath != "" {
		flags = append(flags, "--kubeconfig", c.kubeconfigPath)
	}
	if c.k8sContext != "" {
		flags = append(flags, "--context", c.k8sContext)
	}
	return flags
}

func CanRetryTransfers(kubectlPath string) (bool, error) {
	// Use --client so the call doesn't depend on cluster reachability or
	// a valid kubeconfig — we only parse ClientVersion from the JSON.
	kubectlExec := exec.Command(kubectlPath, "version", "--client", "-o", "json")
	out, err := kubectlExec.Output()
	if err != nil {
		return false, err
	}
	var results k8sVersion
	err = json.Unmarshal([]byte(out), &results)
	if err != nil {
		return false, err
	}

	if results.ClientVersion.Major == "1" {
		parsed, err := strconv.ParseInt(results.ClientVersion.Minor, 10, 32)
		if err != nil {
			return false, err
		}
		// retries flag starts showing up in 1.23.0
		if parsed > 22 {
			return true, nil
		}
		msg := fmt.Sprintf("kubectl version %v no retries available, consider upgrading", results.ClientVersion.GitVersion)
		simplelog.Warning(msg)
		return false, nil
	}
	return false, nil
}

type k8sVersion struct {
	ClientVersion clientVersion `json:"clientVersion"`
}

type clientVersion struct {
	Major      string `json:"major"`
	Minor      string `json:"minor"`
	GitVersion string `json:"gitVersion"`
}

func (c *CliK8sActions) cleanLocal(rawDest string) string {
	// windows does the wrong thing for kubectl here and provides a path with C:\ we need to remove it as kubectl detects this as a remote destination
	return strings.TrimPrefix(rawDest, "C:")
}

func (c *CliK8sActions) resolveContainerName(podName string) (string, error) {
	// Get all container names from the pod
	args := []string{c.kubectlPath}
	args = append(args, c.k8sFlags()...)
	args = append(args, "-n", c.namespace, "get", "pods", string(podName), "-o", `jsonpath={.spec.containers[*].name}`)
	conts, err := c.cli.Execute(false, args...)
	if err != nil {
		return "", err
	}

	containerNames := strings.Fields(strings.TrimSpace(conts))
	if len(containerNames) == 0 {
		return "", fmt.Errorf("no containers found in pod %s", podName)
	}

	// Look for known Dremio container names first
	knownDremioContainers := []string{
		"dremio-coordinator",
		"dremio-master-coordinator",
		"dremio-executor",
		"coordinator",
		"executor",
		"dremio",
	}

	for _, knownContainer := range knownDremioContainers {
		for _, containerName := range containerNames {
			if containerName == knownContainer {
				return containerName, nil
			}
		}
	}

	// If no known container found, look for containers that contain "dremio" in the name
	for _, containerName := range containerNames {
		if strings.Contains(strings.ToLower(containerName), "dremio") {
			return containerName, nil
		}
	}

	// If still no match, fall back to the first container (original behavior)
	return containerNames[0], nil
}

// getContainerName returns the Dremio container for a pod, resolving it once and
// caching the result. Resolution is the only kubectl call that previously ran on
// every HostExecute; memoizing it removes exposure to transient `get pods`
// failures mid-collection. A small retry guards the single cold-miss resolution.
func (c *CliK8sActions) getContainerName(podName string) (string, error) {
	c.m.Lock()
	if c.containerNames == nil {
		c.containerNames = make(map[string]string)
	}
	if name, ok := c.containerNames[podName]; ok {
		c.m.Unlock()
		return name, nil
	}
	c.m.Unlock()

	attempts := 1
	if c.retriesEnabled {
		attempts = 3
	}
	var name string
	var err error
	for i := 0; i < attempts; i++ {
		if name, err = c.resolveContainerName(podName); err == nil {
			break
		}
	}
	if err != nil {
		return "", err
	}

	c.m.Lock()
	c.containerNames[podName] = name
	c.m.Unlock()
	return name, nil
}

func (c *CliK8sActions) Name() string {
	return "Kubectl"
}

func (c *CliK8sActions) Protocol() string {
	return "Kubectl"
}

func (c *CliK8sActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) (err error) {
	container, err := c.getContainerName(hostString)
	if err != nil {
		return fmt.Errorf("unable to get container name: %w", err)
	}
	kubectlArgs := []string{c.kubectlPath}
	kubectlArgs = append(kubectlArgs, c.k8sFlags()...)
	kubectlArgs = append(kubectlArgs, "exec")
	if pat != "" {
		kubectlArgs = append(kubectlArgs, "-i")
	}
	kubectlArgs = append(kubectlArgs, "-n", c.namespace, "-c", container, hostString, "--", "sh", "-c", strings.Join(args, " "))
	return c.cli.ExecuteAndStreamOutput(mask, output, pat, kubectlArgs...)
}

func (c *CliK8sActions) HostExecute(mask bool, hostString string, args ...string) (string, error) {
	return cli.CollectOutput(c.HostExecuteAndStream, mask, hostString, args...)
}

func (c *CliK8sActions) addRetries(args []string) []string {
	if c.retriesEnabled {
		args = append(args, "--retries", "999")
	}
	return args
}

func (c *CliK8sActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(source, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		source = strings.Replace(source, `C:`, ``, 1)
	}
	container, err := c.getContainerName(hostString)
	if err != nil {
		return "", fmt.Errorf("unable to get container name: %w", err)
	}
	args := []string{c.kubectlPath}
	args = append(args, c.k8sFlags()...)
	args = append(args, "cp", "-n", c.namespace, "-c", container)
	args = c.addRetries(args)
	args = append(args, c.cleanLocal(source), fmt.Sprintf("%v:%v", hostString, destination))
	return c.cli.Execute(false, args...)
}

func (c *CliK8sActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *CliK8sActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
	args := []string{c.kubectlPath}
	args = append(args, c.k8sFlags()...)
	args = append(args, "get", "pods", "-n", c.namespace, "-l", c.detectLabelSelector, "--field-selector", "status.phase=Running", "-o", "name")
	out, err := c.cli.Execute(false, args...)
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
	c.m.Lock()
	c.pidHosts[host] = pidFile
	c.m.Unlock()
}

func (c *CliK8sActions) CleanupRemote() error {
	kill := func(host string, pidFile string) {
		if pidFile == "" {
			simplelog.Debugf("pidfile is blank for %v skipping", host)
			return
		}
		container, err := c.getContainerName(host)
		if err != nil {
			simplelog.Warningf("output of container for host %v: %v", host, err)
			return
		}
		kubectlArgs := append([]string{}, c.k8sFlags()...)
		kubectlArgs = append(kubectlArgs, "exec", "-n", c.namespace, "-c", container, host, "--")
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
		kubectlArgs = append([]string{}, c.k8sFlags()...)
		kubectlArgs = append(kubectlArgs, "exec", "-n", c.namespace, "-c", container, host, "--")
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
		c.m.Lock()
		// cancel out so we can skip if it's called again
		c.pidHosts[host] = ""
		c.m.Unlock()
	}
	var wg sync.WaitGroup
	var criticalErrors []string
	coordinators, err := c.GetCoordinators()
	if err != nil {
		msg := fmt.Sprintf("unable to get coordinators for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, coordinator := range coordinators {
			c.m.Lock()
			if v, ok := c.pidHosts[coordinator]; ok {
				wg.Add(1)
				go func(host, pid string) {
					defer wg.Done()
					kill(host, pid)
				}(coordinator, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", coordinator)
			}
			c.m.Unlock()
		}
	}

	executors, err := c.GetExecutors()
	if err != nil {
		msg := fmt.Sprintf("unable to get executors for cleanup %v", err)
		simplelog.Error(msg)
		criticalErrors = append(criticalErrors, msg)
	} else {
		for _, executor := range executors {
			c.m.Lock()
			if v, ok := c.pidHosts[executor]; ok {
				wg.Add(1)
				go func(host, pid string) {
					defer wg.Done()
					kill(host, pid)
				}(executor, v)
			} else {
				simplelog.Errorf("missing key %v in pidHosts skipping host", executor)
			}
			c.m.Unlock()
		}
	}
	wg.Wait()
	if len(criticalErrors) > 0 {
		return fmt.Errorf("critical errors trying to cleanup pods %v", strings.Join(criticalErrors, ", "))
	}
	return nil
}

// StreamFromHost streams the raw bytes of a remote file to writer by executing
// "kubectl exec ... <cmd> '<path>'" as a subprocess where <cmd> is "cat" or
// "gzip -c" depending on useGzip. Binary data integrity is preserved — stdout
// goes directly to writer with no line splitting or encoding.
func (c *CliK8sActions) StreamFromHost(host, remotePath string, writer io.Writer, useGzip bool) error {
	if remotePath == "" {
		return fmt.Errorf("StreamFromHost: remotePath is empty for host %v", host)
	}

	streamCmd := "cat"
	if useGzip {
		streamCmd = "gzip -c"
	}
	simplelog.Infof("StreamFromHost: streaming %v:%v via kubectl exec (cmd=%s)", host, remotePath, streamCmd)

	containerName, err := c.getContainerName(host)
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed to get container for pod %v: %w", host, err)
	}

	escapedPath := strings.ReplaceAll(remotePath, "'", "'\\''")
	args := append([]string{}, c.k8sFlags()...)
	args = append(args, "exec", host, "-n", c.namespace, "-c", containerName, "--", "sh", "-c", fmt.Sprintf("%s '%s'", streamCmd, escapedPath))

	// #nosec G204 -- arguments are controlled by the caller
	cmd := exec.Command(c.kubectlPath, args...)
	cmd.Stdout = writer

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed to create stderr pipe for %v:%v: %w", host, remotePath, err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("StreamFromHost: failed to start kubectl for %v:%v: %w", host, remotePath, err)
	}

	stderrBytes, _ := io.ReadAll(stderrPipe)

	if err := cmd.Wait(); err != nil {
		stderrMsg := strings.TrimSpace(string(stderrBytes))
		if stderrMsg != "" {
			return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w (stderr: %s)", streamCmd, host, remotePath, err, stderrMsg)
		}
		return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w", streamCmd, host, remotePath, err)
	}

	simplelog.Infof("StreamFromHost: completed streaming %v:%v", host, remotePath)
	return nil
}

// DiscoverFiles runs remote discovery shell commands on a K8s pod (via kubectl exec)
// to enumerate log files, config files, GC logs, and the Dremio PID.
func (c *CliK8sActions) DiscoverFiles(host, logDir, confDir string) (*collection.RemoteNodeInfo, error) {
	return collection.RunDiscovery(func(h string, args ...string) (string, error) {
		return c.HostExecute(false, h, args...)
	}, host, logDir, confDir)
}
