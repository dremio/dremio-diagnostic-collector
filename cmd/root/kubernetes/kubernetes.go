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
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/v3/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/v3/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

type KubeArgs struct {
	Namespace     string
	K8SContext    string
	LabelSelector string
}

// NewK8sAPI is the only supported way to initialize the NewK8sAPI struct
// one must pass the path to kubectl
func NewK8sAPI(kubeArgs KubeArgs, hook shutdown.CancelHook) (*KubeCtlAPIActions, error) {
	clientset, config, err := GetClientset(kubeArgs.K8SContext)
	if err != nil {
		return &KubeCtlAPIActions{}, err
	}
	return &KubeCtlAPIActions{
		namespace:      kubeArgs.Namespace,
		client:         clientset,
		config:         config,
		labelSelector:  kubeArgs.LabelSelector,
		hook:           hook,
		pidHosts:       make(map[string]string),
		timeoutMinutes: 30,
	}, nil
}

func GetClientset(k8sContext string) (*kubernetes.Clientset, *rest.Config, error) {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil, err
		}
		kubeConfig = filepath.Join(home, ".kube", "config")
	}
	var config *rest.Config
	_, err := os.Stat(kubeConfig)
	if err != nil {
		// fall back to include config
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else {
		clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeConfig},
			&clientcmd.ConfigOverrides{CurrentContext: k8sContext},
		)
		// for when we have a context of "" we need to log the detected default context so we can display this
		if k8sContext == "" {
			startConfig, err := clientConfig.ConfigAccess().GetStartingConfig()
			if err != nil {
				return nil, nil, err
			}
			simplelog.Infof("current kubernetes context is detected as %v", startConfig.CurrentContext)
		} else {
			simplelog.Infof("using kubernetes context of %v", k8sContext)
		}
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

// KubeCtlAPIActions provides a way to collect and copy files using kubectl
type KubeCtlAPIActions struct {
	namespace      string
	labelSelector  string
	client         *kubernetes.Clientset
	config         *rest.Config
	hook           shutdown.CancelHook
	pidHosts       map[string]string
	timeoutMinutes int
	m              sync.Mutex
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
		exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
		if err != nil {
			simplelog.Warningf("failed getting pidfile %v on host %v: %v", pidFile, host, err)
			return
		}
		var w bytes.Buffer
		var errOut bytes.Buffer
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(30)*time.Second)
		defer cancel()
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
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
		exec, err = remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
		if err != nil {
			simplelog.Warningf("failed killing ddc %v on host %v: %v", w.String(), host, err)
			return
		}
		var buff bytes.Buffer
		ctx, timeout := context.WithTimeout(context.Background(), time.Duration(120)*time.Second)
		defer timeout()
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
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

func (c *KubeCtlAPIActions) GetClient() *kubernetes.Clientset {
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
	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return err
	}
	var buff bytes.Buffer
	writer := &K8SWriter{
		Buff:   &buff,
		Output: output,
	}

	if pat != "" {
		stdIn := bytes.Buffer{}
		if _, err := stdIn.WriteString(pat); err != nil {
			return err
		}
		err = exec.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
			Stdin:  &stdIn,
			Stdout: writer,
			Stderr: writer,
		})
		return err
	}
	return exec.StreamWithContext(c.hook.GetContext(), remotecommand.StreamOptions{
		Stdout: writer,
		Stderr: writer,
	})
}

type K8SWriter struct {
	Output cli.OutputHandler
	Buff   *bytes.Buffer
}

func (w *K8SWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(string(p), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		w.Output(trimmed)
	}
	return w.Buff.Write(p)
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

func (c *KubeCtlAPIActions) HostExecute(mask bool, hostString string, args ...string) (out string, err error) {
	var outBuilder strings.Builder
	writer := func(line string) {
		outBuilder.WriteString(line)
	}
	err = c.HostExecuteAndStream(mask, hostString, writer, "", args...)
	out = outBuilder.String()
	return
}

type TarPipe struct {
	reader     *io.PipeReader
	outStream  *io.PipeWriter
	bytesRead  uint64
	retries    int
	maxRetries int
	src        string
	executor   func(writer *io.PipeWriter, cmdArr []string)
}

func newTarPipe(src string, executor func(writer *io.PipeWriter, cmdArr []string)) *TarPipe {
	t := new(TarPipe)
	t.src = src
	t.maxRetries = 200
	t.executor = executor
	t.initReadFrom(0)
	return t
}

func (t *TarPipe) initReadFrom(n uint64) {
	t.reader, t.outStream = io.Pipe()
	copyCommand := []string{"sh", "-c", fmt.Sprintf("tar cf - %s | tail -c+%d", t.src, n)}
	go func() {
		defer t.outStream.Close()
		t.executor(t.outStream, copyCommand)
	}()
}

func (t *TarPipe) Read(p []byte) (n int, err error) {
	n, err = t.reader.Read(p)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			simplelog.Warning("cancelling transfer")
			return
		}
		if errors.Is(err, context.DeadlineExceeded) {
			simplelog.Warning("timed out stopping retries")
			return
		}
		if t.maxRetries < 0 || t.retries < t.maxRetries {
			// short pause between retries
			time.Sleep(50 * time.Millisecond)
			t.retries++
			simplelog.Warningf("resuming copy at %d bytes, retry %d/%d - %v", t.bytesRead, t.retries, t.maxRetries, err)
			t.initReadFrom(t.bytesRead + 1)
			err = nil
		} else {
			simplelog.Errorf("dropping out copy after %d retries - %v", t.retries, err)
		}
	} else {
		if n > 0 {
			t.bytesRead += uint64(n)
		}
	}
	return
}

func (c *KubeCtlAPIActions) CopyFromHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}

	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return "", fmt.Errorf("failed looking for pod %v: %w", hostString, err)
	}
	simplelog.Infof("transferring from %v:%v to %v", hostString, source, destination)
	executor := func(writer *io.PipeWriter, cmdArr []string) {
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

		exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
		if err != nil {
			msg := fmt.Sprintf("spdy failed: %v", err)
			simplelog.Error(msg)
			return
		}
		var errBuff bytes.Buffer
		duration := time.Duration(c.timeoutMinutes) * time.Minute
		ctx, timeout := context.WithTimeoutCause(c.hook.GetContext(), duration, fmt.Errorf("transferring file %v from host %v timeout exceeded %v", source, hostString, duration))
		defer timeout()
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: writer,
			Stderr: &errBuff,
			Tty:    false,
		})
		if err != nil {
			switch ctx.Err() {
			case context.Canceled:
				simplelog.Warningf("manually cancelled transfer - %v", context.Cause(ctx))
			case context.DeadlineExceeded:
				simplelog.Errorf("%v", context.Cause(ctx))
			default:
				simplelog.Errorf("failed streaming %v - %v", err, errBuff.String())
			}
		}
	}
	reader := newTarPipe(source, executor)
	simplelog.Infof("untarring file '%v' from stdout", destination)
	// has to be filepath or else we get weird outcomes
	if err := archive.ExtractTarStream(reader, filepath.Dir(destination), path.Dir(source)); err != nil {
		return "", fmt.Errorf("unable to copy %w", err)
	}
	simplelog.Infof("file %v untarred fully and transfer is now complete", destination)
	return "", nil
}

func (c *KubeCtlAPIActions) getPrimaryContainer(hostString string) (string, error) {
	pods, err := c.client.CoreV1().Pods(c.namespace).List(context.Background(), meta_v1.ListOptions{})
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
				for _, container := range pod.Spec.Containers {
					if container.Name == knownContainer {
						return container.Name, nil
					}
				}
			}

			// If no known container found, look for containers that contain "dremio" in the name
			for _, container := range pod.Spec.Containers {
				if strings.Contains(strings.ToLower(container.Name), "dremio") {
					return container.Name, nil
				}
			}

			// If still no match, fall back to the first container (original behavior)
			return pod.Spec.Containers[0].Name, nil
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
		defer writer.Close()
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

	exec, err := remotecommand.NewSPDYExecutor(c.config, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("spdy failed: %w", err)
	}
	var errBuff bytes.Buffer
	var outBuff bytes.Buffer

	// hard coding a 4 minute timeout on copy to host we could add a flag but feedback is thare are too many already. Make a PR if you want to change this
	ctx, cancel := context.WithTimeout(c.hook.GetContext(), 4*time.Minute)
	defer cancel()
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
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
		LabelSelector: c.labelSelector,
	})
	if err != nil {
		return podName, err
	}
	count := 0
	for _, p := range podList.Items {
		count++
		if len(p.Spec.Containers) == 0 {
			return podName, fmt.Errorf("unsupported pod %v which has no containers attached", p)
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

		var targetContainer string

		// First, try to find a known Dremio container
		for _, knownContainer := range knownDremioContainers {
			for _, container := range p.Spec.Containers {
				if container.Name == knownContainer {
					targetContainer = container.Name
					break
				}
			}
			if targetContainer != "" {
				break
			}
		}

		// If no known container found, look for containers that contain "dremio" in the name
		if targetContainer == "" {
			for _, container := range p.Spec.Containers {
				if strings.Contains(strings.ToLower(container.Name), "dremio") {
					targetContainer = container.Name
					break
				}
			}
		}

		// If still no match, fall back to the first container (original behavior)
		if targetContainer == "" {
			targetContainer = p.Spec.Containers[0].Name
		}

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

func (c *KubeCtlAPIActions) HelpText() string {
	return "Make sure namespace you use actually has a dremio cluster installed by dremio, if not then this is not supported"
}

func GetClusters() ([]string, error) {
	// we have just chosen to support the default context in the UI where this is used
	clientset, _, err := GetClientset("")
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
			LabelSelector: "role=dremio-cluster-pod",
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
