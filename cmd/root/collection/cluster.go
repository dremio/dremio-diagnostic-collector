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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/dremio/dremio-diagnostic-collector/cmd/root/helpers"
	"github.com/dremio/dremio-diagnostic-collector/cmd/root/kubernetes"
	"github.com/dremio/dremio-diagnostic-collector/pkg/consoleprint"
	"github.com/dremio/dremio-diagnostic-collector/pkg/masking"
	"github.com/dremio/dremio-diagnostic-collector/pkg/shutdown"
	"github.com/dremio/dremio-diagnostic-collector/pkg/simplelog"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var clusterRequestTimeout = 120

func ClusterK8sExecute(hook shutdown.CancelHook, namespace string, cs CopyStrategy, ddfs helpers.Filesystem) error {
	cmds := []string{"nodes", "sc", "pvc", "pv", "service", "endpoints", "pods", "deployments", "statefulsets", "daemonset", "replicaset", "cronjob", "job", "events", "ingress", "limitrange", "resourcequota", "hpa", "pdb", "pc"}
	p, err := cs.CreatePath("kubernetes", "dremio-master", "")
	if err != nil {
		simplelog.Errorf("trying to construct cluster config path %v with error %v", p, err)
		return err
	}

	for _, cmd := range cmds {
		resource := cmd
		out, err := clusterExecuteBytes(hook, namespace, resource)
		if err != nil {
			simplelog.Errorf("when getting cluster config, error was %v", err)
			continue
		}
		text, err := masking.RemoveSecretsFromK8sJSON(out)
		if err != nil {
			simplelog.Errorf("unable to mask secrets for %v in namespace %v returning am empty text due to error '%v'", resource, namespace, err)
			continue
		}

		path := strings.TrimSuffix(p, "dremio-master")
		filename := filepath.Join(path, resource+".json")
		err = ddfs.WriteFile(filename, []byte(text), DirPerms)
		if err != nil {
			simplelog.Errorf("trying to write file %v, error was %v", filename, err)
			continue
		}
		consoleprint.UpdateK8sFiles(cmd)
	}
	return nil
}

func GetClusterLogs(hook shutdown.CancelHook, namespace string, cs CopyStrategy, ddfs helpers.Filesystem, pods []string) error {
	path, err := cs.CreatePath("kubernetes", "container-logs", "")
	if err != nil {
		simplelog.Errorf("trying to construct cluster container log path %v with error %v", path, err)
		return err
	}
	clientSet, _, err := kubernetes.GetClientset()
	if err != nil {
		return err
	}
	// Loop over dremio pods
	for _, podname := range pods {
		podObj, err := clientSet.CoreV1().Pods(namespace).Get(context.Background(), podname, metav1.GetOptions{})
		if err != nil {
			simplelog.Errorf("unable to get pod %v: %v", podname, err)
			continue
		}
		var containers []string
		for _, c := range podObj.Spec.Containers {
			containers = append(containers, c.Name)
		}
		for _, c := range podObj.Spec.InitContainers {
			containers = append(containers, c.Name)
		}
		// Loop over each container, construct a path and log file name
		// write the output of the kubectl logs command to a file
		for _, container := range containers {
			copyContainerLog(hook, cs, ddfs, container, namespace, path, podname)
		}
		consoleprint.UpdateK8sFiles(fmt.Sprintf("pod %v logs", podname))
	}
	return err
}

func copyContainerLog(hook shutdown.CancelHook, cs CopyStrategy, ddfs helpers.Filesystem, container, namespace, path, pod string) {
	client, _, err := kubernetes.GetClientset()
	if err != nil {
		simplelog.Errorf("unable to get k8s client for collecting logs on pod: %v container: %v with error: %v", pod, container, err)
		return
	}
	timeoutDuration := time.Duration(clusterRequestTimeout) * time.Second
	ctx, timeout := context.WithTimeoutCause(hook.GetContext(), timeoutDuration, fmt.Errorf("while copying container %s from pod %s in namespace %s timeout exceeded %v", container, pod, namespace, timeoutDuration))
	defer timeout() // releases resources if slowOperation completes before timeout elapses
	req := client.CoreV1().Pods(namespace).GetLogs(pod, &corev1.PodLogOptions{
		Container: container,
	})
	r, err := req.Stream(ctx)
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			simplelog.Errorf("%v", context.Cause(ctx))
		default:
			simplelog.Errorf("trying to get log from pod: %v container: %v with error: %v", pod, container, err)
			return
		}
	}
	defer r.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, r)
	if err != nil {
		switch ctx.Err() {
		case context.DeadlineExceeded:
			simplelog.Errorf("%v", context.Cause(ctx))
		default:
			simplelog.Errorf("unable to copy log into string for pod: %v container: %v with error: %v", pod, container, err)
			return
		}
	}
	out := buf.String()
	outFile := filepath.Join(path, pod+"-"+container+".txt")
	simplelog.Debugf("getting logs for pod: %v container: %v", pod, container)
	p, err := cs.CreatePath("kubernetes", "container-logs", "")
	if err != nil {
		simplelog.Errorf("trying to create container log path \n%v \nwith error \n%v", p, err)
		return
	}
	// Write out the logs to a file
	err = ddfs.WriteFile(outFile, []byte(out), DirPerms)
	if err != nil {
		simplelog.Errorf("trying to write file %v, error was %v", outFile, err)
	}
}

// Execute commands at the cluster level
// Calls a raw execute function and simply writes out the byte array read from the response
// that comes in directly from kubectl
func clusterExecuteBytes(hook shutdown.CancelHook, namespace, resource string) ([]byte, error) {
	c, _, err := kubernetes.GetClientset()
	if err != nil {
		return []byte(""), err
	}
	options := metav1.ListOptions{}
	var b []byte
	timeoutDuration := 60 * time.Second
	ctx, timeout := context.WithTimeoutCause(hook.GetContext(), timeoutDuration, fmt.Errorf("while getting resource %v in namespace %s timeout exceeded %v", resource, namespace, timeoutDuration))
	defer timeout()
	switch resource {
	case "nodes":
		list, err := c.CoreV1().Nodes().List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Node"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "sc":
		list, err := c.StorageV1().StorageClasses().List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "StorageClass"
			c.APIVersion = "storage.k8s.io/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "pvc":
		list, err := c.CoreV1().PersistentVolumeClaims(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "PersistentVolumeClaim"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "pv":
		list, err := c.CoreV1().PersistentVolumes().List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "PersistentVolume"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "service":
		list, err := c.CoreV1().Services(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Service"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "endpoints":
		list, err := c.CoreV1().Endpoints(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Endpoint"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "pods":
		list, err := c.CoreV1().Pods(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Pod"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "deployments":
		list, err := c.AppsV1().Deployments(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Deployment"
			c.APIVersion = "apps/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "statefulsets":
		list, err := c.AppsV1().StatefulSets(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "StatefulSet"
			c.APIVersion = "apps/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "daemonset":
		list, err := c.AppsV1().StatefulSets(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "DaemonSet"
			c.APIVersion = "apps/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "replicaset":
		list, err := c.AppsV1().ReplicaSets(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "ReplicaSet"
			c.APIVersion = "apps/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "cronjob":
		list, err := c.BatchV1().CronJobs(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "CronJob"
			c.APIVersion = "batch/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "job":
		list, err := c.BatchV1().Jobs(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Job"
			c.APIVersion = "batch/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "events":
		list, err := c.EventsV1().Events(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Event"
			c.APIVersion = "events.k8s.io/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "ingress":
		list, err := c.NetworkingV1().Ingresses(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "Ingress"
			c.APIVersion = "networking.k8s.io/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "limitrange":
		list, err := c.CoreV1().LimitRanges(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "LimitRange"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "resourcequota":
		list, err := c.CoreV1().LimitRanges(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "ResourceQuota"
			c.APIVersion = "v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "hpa":
		list, err := c.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "HorizontalPodAutoscaler"
			c.APIVersion = "autoscaling/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "pdb":
		list, err := c.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "PodDisruptionBudget"
			c.APIVersion = "policy/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	case "pc":
		list, err := c.SchedulingV1().PriorityClasses().List(ctx, options)
		if err != nil {
			switch ctx.Err() {
			case context.DeadlineExceeded:
				return nil, context.Cause(ctx)
			default:
				return []byte(""), err
			}
		}
		list.Kind = "list"
		for i, c := range list.Items {
			c.Kind = "PriorityClass"
			c.APIVersion = "scheduling.k8s.io/v1"
			list.Items[i] = c
		}
		b, err = json.Marshal(list)
		if err != nil {
			return []byte(""), err
		}
	default:
		simplelog.Errorf("resource (%v) does not have an implementation", resource)
	}
	return b, nil

}
