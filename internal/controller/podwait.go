/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
)

const (
	// podWaitRequeue is how often a Job whose pod has not started is looked at again: pod status changes do not
	// trigger a reconcile because the operator does not watch pods.
	podWaitRequeue = 15 * time.Second
	// podWaitGrace is how old a Job without pods must be before "no pod created" is reported when no
	// FailedCreate event explains it.
	podWaitGrace = 30 * time.Second
	// maxWaitMessage bounds the pod detail put into a condition message.
	maxWaitMessage = 300
	// jobNameLabel is set by the Job controller on the pods of a Job.
	jobNameLabel = "batch.kubernetes.io/job-name"
	// flywayContainerName is the container of the Job pod that runs Flyway.
	flywayContainerName = "flyway"

	prefixPodNotCreated   = "pod not created: "
	prefixUnschedulable   = "pod unschedulable: "
	prefixImagePullFailed = "image pull failed: "
	prefixContainerStart  = "container cannot start: "
	backoffPullMessage    = "Back-off pulling image"
)

// reader returns the uncached reader, or the client when none is set.
func (r *MigrationReconciler) reader() client.Reader {
	if r.APIReader != nil {
		return r.APIReader
	}
	return r.Client
}

// podsOfJob lists the pods the Job controller created for the Job, from the API server (not the cache, which
// would start a cluster-wide Pod informer).
func (r *MigrationReconciler) podsOfJob(ctx context.Context, job *batchv1.Job) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	if err := r.reader().List(ctx, list, client.InNamespace(job.Namespace), client.MatchingLabels{jobNameLabel: job.Name}); err != nil {
		return nil, err
	}
	pods := make([]corev1.Pod, 0, len(list.Items))
	for i := range list.Items {
		if metav1.IsControlledBy(&list.Items[i], job) {
			pods = append(pods, list.Items[i])
		}
	}
	return pods, nil
}

// podHasStarted reports whether the Flyway container of the pod ever ran: the pod is past Pending, or the container
// is or was running or has terminated. A pod for which this is false cannot have touched the database. Other
// containers are ignored: the legacy copy-sql init container and injected init containers never run migrations.
func podHasStarted(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodPending && pod.Status.Phase != "" {
		return true
	}
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name != flywayContainerName {
			continue
		}
		if cs.State.Running != nil || cs.State.Terminated != nil || cs.LastTerminationState.Running != nil ||
			cs.LastTerminationState.Terminated != nil || (cs.Started != nil && *cs.Started) {
			return true
		}
	}
	return false
}

// jobMayHaveRun reports whether the Job's own status says a pod already ran or ended, or counts more active pods
// than were listed. Pods are cleaned up independently of the Job, so an absent pod proves nothing then.
func jobMayHaveRun(job *batchv1.Job, listed int) bool {
	st := job.Status
	return st.Succeeded > 0 || st.Failed > 0 || ptr.Deref(st.Terminating, 0) > 0 ||
		(st.UncountedTerminatedPods != nil && (len(st.UncountedTerminatedPods.Succeeded) > 0 || len(st.UncountedTerminatedPods.Failed) > 0)) ||
		int(st.Active) > listed
}

// jobNeverStarted reports whether the Flyway container of the Job never started: the Job has no pod, or only pods
// that are still Pending with it not run, and its status shows no pod that ran or ended. It also returns the pods.
// An error means unknown; callers treat that as started.
func (r *MigrationReconciler) jobNeverStarted(ctx context.Context, job *batchv1.Job) (bool, []corev1.Pod, error) {
	pods, err := r.podsOfJob(ctx, job)
	if err != nil {
		return false, nil, err
	}
	if jobMayHaveRun(job, len(pods)) {
		return false, pods, nil
	}
	for i := range pods {
		if podHasStarted(&pods[i]) {
			return false, pods, nil
		}
	}
	return true, pods, nil
}

// podWaitDetail names why the pods of a running Job have not started, or "" when nothing is wrong that can be
// told apart from a pod that is simply starting. pods must be the Job's pods, none of which has started.
func (r *MigrationReconciler) podWaitDetail(ctx context.Context, job *batchv1.Job, pods []corev1.Pod) string {
	if len(pods) == 0 {
		return r.noPodDetail(ctx, job)
	}
	// newest first, so a replacement pod is described rather than the one it replaced
	slices.SortFunc(pods, func(a, b corev1.Pod) int { return b.CreationTimestamp.Compare(a.CreationTimestamp.Time) })
	pod := &pods[0]
	statuses := append(slices.Clone(pod.Status.InitContainerStatuses), pod.Status.ContainerStatuses...)
	for _, cs := range statuses {
		w := cs.State.Waiting
		if w == nil {
			continue
		}
		switch w.Reason {
		case "ImagePullBackOff", "ErrImagePull", "InvalidImageName":
			return prefixImagePullFailed + truncate(fmt.Sprintf("%s: %s", cs.Name, w.Message), maxWaitMessage)
		case "CreateContainerConfigError", "CreateContainerError":
			return prefixContainerStart + truncate(fmt.Sprintf("%s: %s: %s", cs.Name, w.Reason, w.Message), maxWaitMessage)
		}
	}
	if c := podCondition(pod, corev1.PodScheduled); c != nil && c.Status == corev1.ConditionFalse {
		return prefixUnschedulable + truncate(c.Message, maxWaitMessage)
	}
	return ""
}

// noPodDetail explains a Job without pods from its newest FailedCreate event; without one, from the Job's age.
// Events that cannot be read (no RBAC) are treated like none.
func (r *MigrationReconciler) noPodDetail(ctx context.Context, job *batchv1.Job) string {
	events := &corev1.EventList{}
	err := r.reader().List(ctx, events, client.InNamespace(job.Namespace), client.MatchingFields{"involvedObject.name": job.Name},
		&client.ListOptions{Raw: &metav1.ListOptions{ResourceVersion: "0"}}) // advisory text: a cached read is enough
	if err == nil {
		var newest *corev1.Event
		for i := range events.Items {
			e := &events.Items[i]
			if e.Reason != "FailedCreate" || e.InvolvedObject.Kind != "Job" || e.InvolvedObject.UID != job.UID {
				continue
			}
			if newest == nil || eventTime(e).After(eventTime(newest)) {
				newest = e
			}
		}
		if newest != nil {
			return prefixPodNotCreated + truncate(newest.Message, maxWaitMessage)
		}
	}
	if age := time.Since(job.CreationTimestamp.Time); age > podWaitGrace {
		return fmt.Sprintf("no pod created after %s, see `kubectl describe job %s`", age.Truncate(time.Minute), job.Name)
	}
	return ""
}

func eventTime(e *corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	if !e.EventTime.IsZero() {
		return e.EventTime.Time
	}
	return e.CreationTimestamp.Time
}

func podCondition(pod *corev1.Pod, t corev1.PodConditionType) *corev1.PodCondition {
	for i := range pod.Status.Conditions {
		if pod.Status.Conditions[i].Type == t {
			return &pod.Status.Conditions[i]
		}
	}
	return nil
}

// truncate collapses whitespace and shortens s to at most limit runes.
func truncate(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > limit {
		return string(r[:limit-1]) + "…"
	}
	return s
}

// keepPullDetail keeps the image pull message already in the Progressing condition when the pod has only moved
// from ErrImagePull to ImagePullBackOff, whose message names no cause, so the condition does not flip between them.
func keepPullDetail(migration *flywayv1alpha1.Migration, detail string) string {
	if !strings.HasPrefix(detail, prefixImagePullFailed) || !strings.Contains(detail, backoffPullMessage) {
		return detail
	}
	c := meta.FindStatusCondition(migration.Status.Conditions, flywayv1alpha1.ConditionProgressing)
	if c == nil || c.ObservedGeneration != migration.Generation {
		return detail
	}
	if i := strings.Index(c.Message, prefixImagePullFailed); i >= 0 && !strings.Contains(c.Message, backoffPullMessage) {
		return c.Message[i:]
	}
	return detail
}
