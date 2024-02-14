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
	clientset, config, err := GetCientset()
	if err != nil {
		return &KubectlK8sActions{}, err
	}
	return &KubectlK8sActions{
		namespace: kubeArgs.Namespace,
		client:    clientset,
		config:    config,
	}, nil
}

func GetCientset() (*kubernetes.Clientset, *rest.Config, error) {
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
		fmt.Println("IN cluster config")
		// fall back to include config
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, nil, err
		}
	} else {
		fmt.Printf("using k8s file cluster config %v\n", kubeConfig)

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

func (c *KubectlK8sActions) cleanLocal(rawDest string) string {
	//windows does the wrong thing for kubectl here and provides a path with C:\ we need to remove it as kubectl detects this as a remote destination
	return strings.TrimPrefix(rawDest, "C:")
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
	return exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: writer,
		Stderr: writer,
	})
}

type K8SWriter struct {
	Output cli.OutputHandler
	Buff   *bytes.Buffer
}

func (w *K8SWriter) Write(p []byte) (n int, err error) {
	w.Output(fmt.Sprint(p))
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

func (c *KubectlK8sActions) CopyFromHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	cmd := []string{
		"sh",
		"-c",
		"tar", "cf", "-", source,
	}
	reader, outStream := io.Pipe()
	d := c.cleanLocal(destination)
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
		return "", err
	}
	var errBuff bytes.Buffer
	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: outStream,
		Stderr: &errBuff,
	})
	if err != nil {
		return "", err
	}
	if err := archive.ExtractTarGzStream(reader, d); err != nil {
		return "", err
	}
	return errBuff.String(), nil
}

func (c *KubectlK8sActions) CopyToHost(hostString string, source, destination string) (out string, err error) {
	if strings.HasPrefix(destination, `C:`) {
		// Fix problem seen in https://github.com/kubernetes/kubernetes/issues/77310
		// only replace once because more doesn't make sense
		destination = strings.Replace(destination, `C:`, ``, 1)
	}
	reader, writer := io.Pipe()
	destDir := path.Dir(destination)
	source = c.cleanLocal(source)
	cmd := []string{
		"sh",
		"-c",
		"tar", "xf", "-", "-C", destDir,
	}
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
		return "", err
	}
	var errBuff bytes.Buffer
	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: &errBuff,
		Stderr: &errBuff,
	})
	if err != nil {
		return "", err
	}
	allFiles := func(s string) bool { return true }
	if err := archive.TarGzDirFilteredStream(writer, source, destination, allFiles); err != nil {
		return "", err
	}
	return errBuff.String(), nil
}

func (c *KubectlK8sActions) GetCoordinators() (podName []string, err error) {
	return c.SearchPods(func(container string) bool {
		return strings.Contains(container, "coordinator")
	})
}

func (c *KubectlK8sActions) SearchPods(compare func(container string) bool) (podName []string, err error) {
	podList, err := c.client.CoreV1().Pods(c.namespace).List(context.TODO(), meta_v1.ListOptions{
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
	clientset, _, err := GetCientset()
	if err != nil {
		return []string{}, err
	}
	ns, err := clientset.CoreV1().Namespaces().List(context.TODO(), meta_v1.ListOptions{})
	if err != nil {
		return []string{}, err
	}
	var dremioClusters []string
	for _, n := range ns.Items {
		pods, err := clientset.CoreV1().Pods(n.Name).List(context.TODO(), meta_v1.ListOptions{
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
