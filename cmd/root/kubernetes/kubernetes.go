//	Copyright 2016 The Kubernetes Authors
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
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v4/cmd/root/collection"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/dirs"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v4/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

type KubeArgs struct {
	Namespace      string
	K8SContext     string
	DetectLabelSelector  string
	KubeconfigPath string
}

// NewK8sAPI is the only supported way to initialize the NewK8sAPI struct
// one must pass the path to kubectl
func NewK8sAPI(kubeArgs KubeArgs, hook shutdown.CancelHook) (*KubeCtlAPIActions, error) {
	clientset, config, err := GetClientset(kubeArgs.K8SContext, kubeArgs.KubeconfigPath)
	if err != nil {
		return &KubeCtlAPIActions{}, err
	}
	return &KubeCtlAPIActions{
		namespace:      kubeArgs.Namespace,
		client:         clientset,
		config:         config,
		detectLabelSelector:  kubeArgs.DetectLabelSelector,
		hook:           hook,
		pidHosts:       make(map[string]string),
		containerCache: make(map[string]string),
		timeoutMinutes: 30,
		protocol:       "SPDY",
		spdyExecutorFn: func(config *rest.Config, method string, u *url.URL) (remotecommand.Executor, error) {
			return remotecommand.NewSPDYExecutor(config, method, u)
		},
	}, nil
}

// GetClientset returns a clientset and rest.Config using the supplied
// kubeconfigPath if non-empty (with precedence: explicit → $KUBECONFIG →
// ~/.kube/config). Falls back to in-cluster config when no kubeconfig
// file exists.
func GetClientset(k8sContext, kubeconfigPath string) (*kubernetes.Clientset, *rest.Config, error) {
	resolved := resolveKubeconfigPath(kubeconfigPath)
	var config *rest.Config
	if resolved == "" {
		// No path determinable — try in-cluster.
		var err error
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else if _, err := os.Stat(resolved); err != nil { // #nosec G703 -- resolved is user-supplied kubeconfig path
		// File does not exist — fall back to in-cluster.
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else {
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: resolved},
			&clientcmd.ConfigOverrides{CurrentContext: k8sContext},
		)
		if k8sContext == "" {
			startConfig, err := clientConfig.ConfigAccess().GetStartingConfig()
			if err != nil {
				return nil, nil, err
			}
			simplelog.Infof("current kubernetes context is detected as %v", startConfig.CurrentContext)
		} else {
			simplelog.Infof("using kubernetes context of %v", k8sContext)
		}
		var err error
		config, err = clientConfig.ClientConfig()
		if err != nil {
			return nil, nil, err
		}
	}
	simplelog.Debugf("connection to kubernetes API: %v", config.Host)
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return clientset, config, nil
}

// ExecutorFactory creates a remotecommand.Executor for the given HTTP method and URL.
type ExecutorFactory func(config *rest.Config, method string, url *url.URL) (remotecommand.Executor, error)

// KubeCtlAPIActions provides a way to collect and copy files using kubectl
type KubeCtlAPIActions struct {
	namespace      string
	detectLabelSelector  string
	client               kubernetes.Interface
	config         *rest.Config
	hook           shutdown.CancelHook
	pidHosts       map[string]string
	containerCache map[string]string // cached pod→container name lookups
	timeoutMinutes int
	m              sync.Mutex
	spdyExecutorFn ExecutorFactory
	protocol       string // "SPDY"
}

// newExecutor creates a SPDY executor for remote command execution.
func (c *KubeCtlAPIActions) newExecutor(method string, u *url.URL) (remotecommand.Executor, error) {
	return c.spdyExecutorFn(c.config, method, u)
}

func (c *KubeCtlAPIActions) Protocol() string {
	c.m.Lock()
	defer c.m.Unlock()
	return c.protocol
}

func (c *KubeCtlAPIActions) SetHostPid(host, pidFile string) {
	c.m.Lock()
	defer c.m.Unlock()
	c.pidHosts[host] = pidFile
}

func (c *KubeCtlAPIActions) CleanupRemote() error {
	kill := func(host string, pidFile string) {
		if pidFile == "" {
			simplelog.Debugf("pidfile is blank for %v skipping", host)
			return
		}
		containerName, err := c.getPrimaryContainer(host)
		if err != nil {
			simplelog.Warningf("failed looking for pod %v: %v", host, err)
			return
		}
		req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(host).
			Namespace(c.namespace).SubResource("exec")
		cmd := []string{
			"sh",
			"-c",
			fmt.Sprintf("cat  %v", pidFile),
		}
		option := &v1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}
		req = req.VersionedParams(
			option,
			scheme.ParameterCodec,
		)
		executor, err := c.newExecutor("POST", req.URL())
		if err != nil {
			simplelog.Warningf("failed getting pidfile %v on host %v: %v", pidFile, host, err)
			return
		}
		var w bytes.Buffer
		var errOut bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(30)*time.Second)
		defer cancel()
		err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: &w,
			Stderr: &errOut,
		})
		if err != nil {
			simplelog.Warningf("failed getting pidfile %v on host %v: %v - %v", pidFile, host, err, errOut.String())
			return
		}

		req = c.client.CoreV1().RESTClient().Post().Resource("pods").Name(host).
			Namespace(c.namespace).SubResource("exec")
		cmd = []string{
			"sh",
			"-c",
			fmt.Sprintf("kill -15 %v", w.String()),
		}
		option = &v1.PodExecOptions{
			Container: containerName,
			Command:   cmd,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}
		req = req.VersionedParams(
			option,
			scheme.ParameterCodec,
		)
		executor, err = c.newExecutor("POST", req.URL())
		if err != nil {
			simplelog.Warningf("failed killing ddc %v on host %v: %v", w.String(), host, err)
			return
		}
		var buff bytes.Buffer
		ctx, timeout := context.WithTimeout(context.Background(), time.Duration(120)*time.Second)
		defer timeout()
		err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdout: &buff,
			Stderr: &buff,
		})
		if err != nil {
			simplelog.Warningf("failed killing ddc %v on host %v: %v - %v", w.String(), host, err, buff.String())
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
	var criticalErrors []string

	var wg sync.WaitGroup
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

func (c *KubeCtlAPIActions) GetClient() kubernetes.Interface {
	return c.client
}

func (c *KubeCtlAPIActions) Name() string {
	return "Kube API"
}

func (c *KubeCtlAPIActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, pat string, args ...string) (err error) {
	cmd := []string{
		"sh",
		"-c",
		strings.Join(args, " "),
	}
	// cmd := args
	logArgs(mask, args)
	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return fmt.Errorf("failed looking for pod %v: %w", hostString, err)
	}
	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(hostString).
		Namespace(c.namespace).SubResource("exec")
	option := &v1.PodExecOptions{
		Container: containerName,
		Command:   cmd,
		Stdin:     pat != "",
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}

	req = req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)
	executor, err := c.newExecutor("POST", req.URL())
	if err != nil {
		return err
	}
	writer := &K8SWriter{
		Output: output,
	}

	if pat != "" {
		stdIn := bytes.Buffer{}
		if _, err := stdIn.WriteString(pat); err != nil {
			return err
		}
		err = executor.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
			Stdin:  &stdIn,
			Stdout: writer,
			Stderr: writer,
		})
		writer.Flush()
		return err
	}
	err = executor.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
		Stdout: writer,
		Stderr: writer,
	})
	writer.Flush()
	return err
}

type K8SWriter struct {
	Output  cli.OutputHandler
	partial strings.Builder // buffers an incomplete line across Write calls
}

func (w *K8SWriter) Write(p []byte) (n int, err error) {
	data := string(p)
	for {
		idx := strings.Index(data, "\n")
		if idx < 0 {
			// No newline — buffer the partial line for the next Write call.
			w.partial.WriteString(data)
			break
		}
		// Complete line: partial prefix + data up to \n.
		w.partial.WriteString(data[:idx])
		w.Output(w.partial.String())
		w.partial.Reset()
		data = data[idx+1:]
	}
	return len(p), nil
}

// Flush sends any remaining partial line to the output handler.
// Call this after streaming completes to avoid losing the last line
// when the remote command's output doesn't end with a newline.
func (w *K8SWriter) Flush() {
	if w.partial.Len() > 0 {
		w.Output(w.partial.String())
		w.partial.Reset()
	}
}

func logArgs(mask bool, args []string) {
	// log out args, mask if needed
	if mask {
		maskedOutput := masking.MaskPAT(strings.Join(args, " "))
		simplelog.Infof("args: %v", maskedOutput)
	} else {
		simplelog.Infof("args: %v", strings.Join(args, " "))
	}
}

func (c *KubeCtlAPIActions) HostExecute(mask bool, hostString string, args ...string) (string, error) {
	return cli.CollectOutput(c.HostExecuteAndStream, mask, hostString, args...)
}

// knownDremioContainers lists container names that identify a Dremio container,
// ordered by specificity. Used by findDremioContainer.
var knownDremioContainers = []string{
	"dremio-coordinator",
	"dremio-master-coordinator",
	"dremio-executor",
	"coordinator",
	"executor",
	"dremio",
}

// findDremioContainer picks the primary Dremio container from a pod's container list
// using a 3-tier lookup: exact match against known names, substring "dremio", then first container.
func findDremioContainer(containers []v1.Container) string {
	for _, known := range knownDremioContainers {
		for i := range containers {
			if containers[i].Name == known {
				return containers[i].Name
			}
		}
	}
	for i := range containers {
		if strings.Contains(strings.ToLower(containers[i].Name), "dremio") {
			return containers[i].Name
		}
	}
	if len(containers) > 0 {
		return containers[0].Name
	}
	return ""
}

func (c *KubeCtlAPIActions) getPrimaryContainer(hostString string) (string, error) {
	c.m.Lock()
	if cached, ok := c.containerCache[hostString]; ok {
		c.m.Unlock()
		return cached, nil
	}
	c.m.Unlock()

	pods, err := c.client.CoreV1().Pods(c.namespace).List(context.Background(), meta_v1.ListOptions{LabelSelector: c.detectLabelSelector})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod match for %v", hostString)
	}

	for _, pod := range pods.Items {
		if pod.Name == hostString {
			if len(pod.Spec.Containers) == 0 {
				return "", fmt.Errorf("no containers found in pod %s", hostString)
			}
			name := findDremioContainer(pod.Spec.Containers)
			c.m.Lock()
			c.containerCache[hostString] = name
			c.m.Unlock()
			return name, nil
		}
	}
	return "", fmt.Errorf("pod %s not found", hostString)
}

func (c *KubeCtlAPIActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(source, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		source = strings.Replace(source, `C:`, ``, 1)
	}
	if _, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("%s doesn't exist in local filesystem", source)
	}
	// this is rather not obvious but waiting closing the reader will hang the process, so do not close it on defer
	// see this thread for all of the complicated problems we can encounter using SPDY https://github.com/kubernetes/client-go/issues/554
	reader, writer := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func(src string) {
		defer writer.Close() //nolint:errcheck // pipe writer close error is non-fatal
		defer wg.Done()
		// use filepath here or else we will get surprises
		srcDir := filepath.Dir(src)
		simplelog.Debugf("k8s API transfer archiving %v to transfer file %v to make it visible on host %v", srcDir, src, hostString)
		if err := archive.TarGzDirFilteredStream(srcDir, writer, func(s string) bool {
			return s == src
		}); err != nil {
			simplelog.Errorf("unable to archive %v", err)
		}
	}(source)
	// use path here since it's always going to a linux destination
	destDir := path.Dir(destination)
	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return "", fmt.Errorf("failed looking for pod %v: %w", hostString, err)
	}
	simplelog.Debugf("k8s API transfer unarchive %v to send file %v to make it visible on host %v", destDir, destination, hostString)
	cmdArr := []string{"sh", "-c", fmt.Sprintf("tar -xzmf - -C %v", destDir)}
	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(hostString).
		Namespace(c.namespace).SubResource("exec")
	option := &v1.PodExecOptions{
		Container: containerName,
		Command:   cmdArr,
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}

	req.VersionedParams(
		option,
		scheme.ParameterCodec,
	)

	executor, err := c.newExecutor("POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("executor creation failed: %w", err)
	}
	var errBuff bytes.Buffer
	var outBuff bytes.Buffer

	// hard coding a 4 minute timeout on copy to host we could add a flag but feedback is thare are too many already. Make a PR if you want to change this
	ctx, cancel := context.WithTimeout(c.hook.GetContext(), 4*time.Minute)
	defer cancel()
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: &outBuff,
		Stderr: &errBuff,
		Tty:    false,
	})
	if err != nil {
		// we are chosing not ot wait here, the theory being that depending on how the error occurred we could see a deadlock
		return "", fmt.Errorf("failed streaming %w - %v", err, errBuff.String()+outBuff.String())
	}
	wg.Wait()
	return errBuff.String() + outBuff.String(), nil
}

func (c *KubeCtlAPIActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *KubeCtlAPIActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
	podList, err := c.client.CoreV1().Pods(c.namespace).List(context.Background(), meta_v1.ListOptions{
		LabelSelector: c.detectLabelSelector,
	})
	if err != nil {
		return podName, err
	}
	count := 0
	for _, p := range podList.Items {
		if p.Status.Phase != v1.PodRunning {
			simplelog.Debugf("skipping pod %v in phase %v", p.Name, p.Status.Phase)
			continue
		}
		count++
		if len(p.Spec.Containers) == 0 {
			return podName, fmt.Errorf("unsupported pod %v which has no containers attached", p)
		}

		targetContainer := findDremioContainer(p.Spec.Containers)
		if compare(targetContainer) {
			podName = append(podName, p.Name)
		}
	}

	// so 100 pods would get 63 minutes to transfer before the transfers timed out
	c.m.Lock()
	c.timeoutMinutes = (count / 3) + 30
	c.m.Unlock()
	sort.Strings(podName)
	return podName, nil
}

func (c *KubeCtlAPIActions) GetExecutors() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return container == "dremio-executor"
	})
}

// DiscoverPods connects to a K8s cluster and lists running Dremio pods,
// classifying each as coordinator or executor based on container names.
// It uses the same container-name matching logic as SearchPods.
// This is a standalone function that does not require a full Collector.
func DiscoverPods(k8sContext, namespace, labelSelector, kubeconfigPath string) (coordinators []string, executors []string, err error) {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("DiscoverPods: failed to get clientset: %w", err)
	}
	podList, err := clientset.CoreV1().Pods(namespace).List(context.Background(), meta_v1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("DiscoverPods: failed to list pods: %w", err)
	}

	for _, p := range podList.Items {
		if p.Status.Phase != v1.PodRunning {
			continue
		}
		if len(p.Spec.Containers) == 0 {
			continue
		}

		targetContainer := findDremioContainer(p.Spec.Containers)

		// Classify: coordinator-like containers go to coordinators, else executors.
		if strings.Contains(targetContainer, "coordinator") || strings.Contains(targetContainer, "master") {
			coordinators = append(coordinators, p.Name)
		} else {
			executors = append(executors, p.Name)
		}
	}

	sort.Strings(coordinators)
	sort.Strings(executors)
	return coordinators, executors, nil
}

// StreamFromHost streams the raw bytes of a remote file to writer by executing
// "cat" (or "gzip -c" when useGzip is true) via SPDY exec. Binary data integrity
// is preserved — stdout goes directly to writer with no line splitting or encoding.
func (c *KubeCtlAPIActions) StreamFromHost(host, remotePath string, writer io.Writer, useGzip bool) error {
	if remotePath == "" {
		return fmt.Errorf("StreamFromHost: remotePath is empty for host %v", host)
	}

	streamCmd := "cat"
	if useGzip {
		streamCmd = "gzip -1 -c"
	}
	simplelog.Infof("StreamFromHost: streaming %v:%v via K8s SPDY exec (cmd=%s)", host, remotePath, streamCmd)

	containerName, err := c.getPrimaryContainer(host)
	if err != nil {
		return fmt.Errorf("StreamFromHost: failed looking for pod %v: %w", host, err)
	}

	// Escape single quotes in remotePath to prevent shell injection:
	// replace ' with '\'' (end quote, escaped quote, start quote).
	escapedPath := strings.ReplaceAll(remotePath, "'", "'\\''")
	cmd := []string{"sh", "-c", fmt.Sprintf("%s '%s'", streamCmd, escapedPath)}

	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(host).
		Namespace(c.namespace).SubResource("exec")
	option := &v1.PodExecOptions{
		Container: containerName,
		Command:   cmd,
		Stdin:     false,
		Stdout:    true,
		Stderr:    true,
		TTY:       false,
	}
	req = req.VersionedParams(option, scheme.ParameterCodec)

	executor, err := c.newExecutor("POST", req.URL())
	if err != nil {
		return fmt.Errorf("StreamFromHost: executor creation failed for %v:%v: %w", host, remotePath, err)
	}

	var stderrBuf bytes.Buffer
	c.m.Lock()
	streamTimeout := time.Duration(c.timeoutMinutes) * time.Minute
	c.m.Unlock()
	ctx, cancel := context.WithTimeout(c.hook.GetContext(), streamTimeout)
	defer cancel()

	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: writer,
		Stderr: &stderrBuf,
	})
	if err != nil {
		stderrMsg := strings.TrimSpace(stderrBuf.String())
		if stderrMsg != "" {
			return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w (stderr: %s)", streamCmd, host, remotePath, err, stderrMsg)
		}
		return fmt.Errorf("StreamFromHost: %s failed on %v:%v: %w", streamCmd, host, remotePath, err)
	}

	simplelog.Infof("StreamFromHost: completed streaming %v:%v", host, remotePath)
	return nil
}

func (c *KubeCtlAPIActions) HelpText() string {
	return "Make sure namespace you use actually has a dremio cluster installed by dremio, if not then this is not supported"
}

// DiscoverFiles runs remote discovery shell commands on a K8s pod to enumerate
// log files, config files, GC logs, and the Dremio PID.
func (c *KubeCtlAPIActions) DiscoverFiles(host, logDir, confDir string) (*collection.RemoteNodeInfo, error) {
	return collection.RunDiscovery(func(h string, args ...string) (string, error) {
		return c.HostExecute(false, h, args...)
	}, host, logDir, confDir)
}

// CheckRBAC verifies minimum RBAC permissions for DDC collection on the given namespace.
// It checks: get pods, list pods, create pods/exec.
// Returns an error listing any missing permissions.
func CheckRBAC(k8sContext, namespace, kubeconfigPath string) error {
	checks := []struct {
		verb     string
		resource string
	}{
		{"get", "pods"},
		{"list", "pods"},
		{"create", "pods/exec"},
	}
	var missing []string
	for _, check := range checks {
		var args []string
		if kubeconfigPath != "" {
			args = append(args, "--kubeconfig", kubeconfigPath)
		}
		if k8sContext != "" {
			args = append(args, "--context", k8sContext)
		}
		args = append(args, "auth", "can-i", check.verb, check.resource, "-n", namespace)
		cmd := exec.Command("kubectl", args...)
		output, err := cmd.CombinedOutput()
		result := strings.TrimSpace(string(output))
		if err != nil || result != "yes" {
			missing = append(missing, fmt.Sprintf("%s %s", check.verb, check.resource))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("insufficient RBAC permissions in namespace %q: missing %s. Ensure your ServiceAccount has a Role/ClusterRole with these permissions", namespace, strings.Join(missing, ", "))
	}
	return nil
}

// ListContexts parses the kubeconfig and returns all context names (sorted)
// and the current-context. If kubeconfigPath is non-empty, it is used as
// the explicit kubeconfig source (overriding $KUBECONFIG and ~/.kube/config).
// If no kubeconfig file exists (e.g. running in-cluster), it returns
// (nil, "", nil) so the caller can skip the context picker.
func ListContexts(kubeconfigPath string) (contexts []string, currentContext string, err error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if resolved := resolveKubeconfigPath(kubeconfigPath); resolved != "" {
		loadingRules.ExplicitPath = resolved
	}
	config, err := loadingRules.Load()
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil
		}
		// If the default kubeconfig path simply doesn't exist, Load returns
		// a not-found error — treat it as a graceful skip.
		if strings.Contains(err.Error(), "no such file or directory") ||
			strings.Contains(err.Error(), "The system cannot find the file specified") ||
			strings.Contains(err.Error(), "The system cannot find the path specified") {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("failed to load kubeconfig: %w", err)
	}
	for name := range config.Contexts {
		contexts = append(contexts, name)
	}
	sort.Strings(contexts)
	return contexts, config.CurrentContext, nil
}

func GetClusters(k8sContext, labelSelector, kubeconfigPath string) ([]string, error) {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
	if err != nil {
		return []string{}, err
	}
	ns, err := clientset.CoreV1().Namespaces().List(context.Background(), meta_v1.ListOptions{})
	if err != nil {
		return []string{}, err
	}
	var dremioClusters []string
	for _, n := range ns.Items {
		pods, err := clientset.CoreV1().Pods(n.Name).List(context.Background(), meta_v1.ListOptions{
			LabelSelector: labelSelector,
		})
		if err != nil {
			return []string{}, err
		}
		if len(pods.Items) > 0 {
			dremioClusters = append(dremioClusters, n.Name)
		}
	}
	sort.Strings(dremioClusters)
	return dremioClusters, nil
}

// VerifyConnectivity issues a lightweight namespace-list call against the
// cluster identified by the given kubeconfigPath and k8sContext. Used by
// the TUI to confirm the user's kubeconfig actually reaches a cluster
// before moving on. The returned error wraps the underlying transport or
// auth error verbatim — callers should surface it to the user.
func VerifyConnectivity(kubeconfigPath, k8sContext string) error {
	clientset, _, err := GetClientset(k8sContext, kubeconfigPath)
	if err != nil {
		return fmt.Errorf("kubeconfig load: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := clientset.CoreV1().Namespaces().List(ctx, meta_v1.ListOptions{Limit: 1}); err != nil {
		return fmt.Errorf("cluster reachable check failed: %w", err)
	}
	return nil
}

// resolveKubeconfigPath returns the effective kubeconfig file path with
// precedence:
//
//  1. explicit (e.g. --kubeconfig flag or TUI input), tilde-expanded
//  2. $KUBECONFIG env var, tilde-expanded
//  3. $HOME/.kube/config
//
// Returns "" only if all three are unavailable (e.g. no home dir).
// Matches `kubectl --kubeconfig` precedence so flag overrides env.
func resolveKubeconfigPath(explicit string) string {
	if explicit != "" {
		return dirs.ExpandTilde(explicit)
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return dirs.ExpandTilde(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kube", "config")
}
