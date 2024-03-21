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
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/cli"
	"github.com/dremio/dremio-diagnostic-collector/pkg/archive"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	v1 "k8s.io/api/core/v1"
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/scheme"
)

type KubeArgs struct {
	Namespace string
}

// NewKubectlK8sActions is the only supported way to initialize the KubectlK8sActions struct
// one must pass the path to kubectl
func NewKubectlK8sActions(kubeArgs KubeArgs) (*KubectlK8sActions, error) {
	clientset, config, err := GetClientset()
	if err != nil {
		return &KubectlK8sActions{}, err
	}
	return &KubectlK8sActions{
		namespace: kubeArgs.Namespace,
		client:    clientset,
		config:    config,
	}, nil
}

func GetClientset() (*kubernetes.Clientset, *rest.Config, error) {
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
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, nil, err
		}
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, err
	}
	return clientset, config, nil
}

// KubectlK8sActions provides a way to collect and copy files using kubectl
type KubectlK8sActions struct {
	namespace string
	client    *kubernetes.Clientset
	config    *rest.Config
}

func (c *KubectlK8sActions) GetClient() *kubernetes.Clientset {
	return c.client
}

func (c *KubectlK8sActions) Name() string {
	return "Kube API"
}

func (c *KubectlK8sActions) HostExecuteAndStream(mask bool, hostString string, output cli.OutputHandler, args ...string) (err error) {
	cmd := []string{
		"sh",
		"-c",
		strings.Join(args, " "),
	}
	logArgs(mask, args)
	req := c.client.CoreV1().RESTClient().Post().Resource("pods").Name(hostString).
		Namespace(c.namespace).SubResource("exec")
	option := &v1.PodExecOptions{
		Command: cmd,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}

	req.VersionedParams(
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
	return exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
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
		w.Output(strings.TrimSpace(line))
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

func (c *KubectlK8sActions) HostExecute(mask bool, hostString string, args ...string) (out string, err error) {
	var outBuilder strings.Builder
	writer := func(line string) {
		outBuilder.WriteString(line)
	}
	err = c.HostExecuteAndStream(mask, hostString, writer, args...)
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
	t.maxRetries = 50
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
		if t.maxRetries < 0 || t.retries < t.maxRetries {
			// short pause between retries
			time.Sleep(100 * time.Millisecond)
			t.retries++
			simplelog.Warningf("resuming copy at %d bytes, retry %d/%d - %v", t.bytesRead, t.retries, t.maxRetries, err)
			t.initReadFrom(t.bytesRead + 1)
			err = nil
		} else {
			simplelog.Errorf("dropping out copy after %d retries - %v", t.retries, err)
		}
	} else {
		t.bytesRead += uint64(n)
	}
	return
}

func (c *KubectlK8sActions) CopyFromHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}

	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return "", fmt.Errorf("failed looking for pod %v: %v", hostString, err)
	}
	simplelog.Infof("transfering from %v:%v to %v", hostString, source, destination)
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
		// hard coding a 1 hour timeout, we could add a flag but feedback is thare are too many already. Make a PR if you want to change this
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
		defer cancel()
		err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: writer,
			Stderr: &errBuff,
			Tty:    false,
		})
		if err != nil {
			msg := fmt.Sprintf("failed streaming %v - %v", err, errBuff.String())
			simplelog.Error(msg)
			// ok this is non intuitive but this may fail..but not really fail
			//failed = errors.New(msg)
		}
	}
	reader := newTarPipe(source, executor)
	if err := archive.ExtractTarStream(reader, path.Dir(destination), path.Dir(source)); err != nil {
		return "", fmt.Errorf("unable to copy %v", err)
	}
	return "", nil
}

func (c *KubectlK8sActions) getPrimaryContainer(hostString string) (string, error) {
	pods, err := c.client.CoreV1().Pods(c.namespace).List(context.Background(), meta_v1.ListOptions{})
	if err != nil {
		return "", err
	}
	if len(pods.Items) == 0 {
		return "", fmt.Errorf("no pod match for %v", hostString)
	}
	var containerName string
	for _, pod := range pods.Items {
		if pod.Name == hostString {
			containerName = pod.Spec.Containers[0].Name
		}
	}
	return containerName, nil
}

func (c *KubectlK8sActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(source, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(source, `C:`, ``, 1)
	}
	if _, err := os.Stat(source); err != nil {
		return "", fmt.Errorf("%s doesn't exist in local filesystem", source)
	}
	// this is rather not obvious but waiting closing the reader will hang the process, so do not close it on defer
	// see this thread for all of the complicated problems we can encounter using SPDY https://github.com/kubernetes/client-go/issues/554
	reader, writer := io.Pipe()
	var wg sync.WaitGroup
	wg.Add(1)
	go func(src string, dest string, w io.WriteCloser) {
		defer writer.Close()
		defer wg.Done()
		srcDir := path.Dir(src)
		if err := archive.TarGzDirFilteredStream(srcDir, writer, func(s string) bool {
			return s == src
		}); err != nil {
			simplelog.Errorf("unable to archive %v", err)
		}
	}(source, destination, writer)
	destDir := path.Dir(destination)
	containerName, err := c.getPrimaryContainer(hostString)
	if err != nil {
		return "", fmt.Errorf("failed looking for pod %v: %v", hostString, err)
	}
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
		return "", fmt.Errorf("spdy failed: %v", err)
	}
	var errBuff bytes.Buffer
	var outBuff bytes.Buffer

	// hard coding a 4 minute timeout on copy to host we could add a flag but feedback is thare are too many already. Make a PR if you want to change this
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: &outBuff,
		Stderr: &errBuff,
		Tty:    false,
	})
	if err != nil {
		// we are chosing not ot wait here, the theory being that depending on how the error occurred we could see a deadlock
		return "", fmt.Errorf("failed streaming %v - %v", err, errBuff.String()+outBuff.String())
	}
	wg.Wait()
	return errBuff.String() + outBuff.String(), nil
}

func (c *KubectlK8sActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *KubectlK8sActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
	podList, err := c.client.CoreV1().Pods(c.namespace).List(context.Background(), meta_v1.ListOptions{
		LabelSelector: "role=dremio-cluster-pod",
	})
	if err != nil {
		return podName, err
	}
	for _, p := range podList.Items {
		if len(p.Spec.Containers) == 0 {
			return podName, fmt.Errorf("unsupported pod %v which has no containers attached", p)
		}
		containerName := p.Spec.Containers[0]
		if compare(containerName.Name) {
			podName = append(podName, p.Name)
		}
	}
	sort.Strings(podName)
	return podName, nil
}

func (c *KubectlK8sActions) GetExecutors() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return container == "dremio-executor"
	})
}

func (c *KubectlK8sActions) HelpText() string {
	return "Make sure namespace you use actually has a dremio cluster installed by dremio, if not then this is not supported"
}

func GetClusters() ([]string, error) {
	clientset, _, err := GetClientset()
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
