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
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
)

// fakeLogReader serves fixture logs by pod name and records the reads.
type fakeLogReader struct {
	t     *testing.T
	logs  map[string]string // pod name -> fixture file
	err   error
	calls []string
	// deadline is how far away the context's deadline was on the last read; 0 when it had none.
	deadline time.Duration
}

func (f *fakeLogReader) ReadLog(ctx context.Context, namespace, pod, container string, limitBytes int64) ([]byte, error) {
	f.calls = append(f.calls, pod)
	if container != flywayContainerName || limitBytes != maxFlywayLogBytes || namespace != statusKey.Namespace {
		f.t.Errorf("read %s/%s container %q limit %d, want container %q limit %d", namespace, pod, container, limitBytes, flywayContainerName, maxFlywayLogBytes)
	}
	f.deadline = 0
	if d, ok := ctx.Deadline(); ok {
		f.deadline = time.Until(d)
	}
	if f.err != nil {
		return nil, f.err
	}
	fixture, ok := f.logs[pod]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{Resource: "pods"}, pod)
	}
	return readFixture(f.t, fixture), nil
}

func withLogs(h *statusHarness, logs map[string]string) *fakeLogReader {
	f := &fakeLogReader{t: h.t, logs: logs}
	h.r.LogReader = f
	return f
}

// ended gives the pod the phase, created at the given offset from now (pods are told apart by age).
func ended(phase corev1.PodPhase, age time.Duration) func(p *corev1.Pod) {
	return func(p *corev1.Pod) {
		p.CreationTimestamp = metav1.NewTime(time.Now().Add(-age).Truncate(time.Second))
		p.Status.Phase = phase
	}
}

// podLists records the resourceVersion of every pod list, and forbids pod lists when forbid is set.
func podLists(forbid bool, rvs *[]string) interceptor.Funcs {
	return interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*corev1.PodList); ok {
			o := &client.ListOptions{}
			o.ApplyOptions(opts)
			rv := ""
			if o.Raw != nil {
				rv = o.Raw.ResourceVersion
			}
			*rvs = append(*rvs, rv)
			if forbid {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", nil)
			}
		}
		return c.List(ctx, list, opts...)
	}}
}

func (h *statusHarness) message() string {
	h.t.Helper()
	return meta.FindStatusCondition(h.migration().Status.Conditions, flywayv1alpha1.ConditionProgressing).Message
}

func TestSchemaVersionFromSucceededJob(t *testing.T) {
	var rvs []string
	h := newMetricsHarness(t, nil, podLists(false, &rvs))
	logs := withLogs(h, map[string]string{"p": "default-run.log"})
	h.reconcile()
	h.addPod("p", ended(corev1.PodSucceeded, time.Minute))
	h.finishJob(batchv1.JobComplete, 0)

	rvs = nil
	m := h.reconcile()
	// fresh: the API server's watch cache may not show the pod's final phase yet
	if len(rvs) != 1 || rvs[0] != "" {
		t.Errorf("pod lists for the log read = %q, want one fresh list", rvs)
	}
	h.expect(m, flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
	if m.Status.SchemaVersion != "2" {
		t.Errorf("schemaVersion = %q, want 2", m.Status.SchemaVersion)
	}
	if logs.deadline <= 0 || logs.deadline > flywayLogTimeout {
		t.Errorf("log read deadline in %v, want within %v", logs.deadline, flywayLogTimeout)
	}
	writes := h.statusUpdates
	for range 3 {
		h.reconcile()
	}
	if len(logs.calls) != 1 {
		t.Errorf("log reads = %v, want one", logs.calls)
	}
	if h.statusUpdates != writes {
		t.Errorf("status writes after the result was recorded = %d, want 0", h.statusUpdates-writes)
	}
}

// A later run that fails, is running, or reports no version keeps the recorded version.
func TestSchemaVersionKept(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	withLogs(h, map[string]string{"g1": "default-run.log", "g2": "migrate-error.log", "g3": "validate.log", "g4": "text-output.log"})
	h.reconcile()
	h.addPod("g1", ended(corev1.PodSucceeded, 4*time.Minute))
	h.finishJob(batchv1.JobComplete, 0)
	h.reconcile()

	expectVersion := func(step string) {
		t.Helper()
		if v := h.migration().Status.SchemaVersion; v != "2" {
			t.Errorf("%s: schemaVersion = %q, want 2 kept", step, v)
		}
	}
	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job replaced
	h.reconcile() // created
	h.reconcile() // running
	expectVersion("running")
	h.addPod("g2", ended(corev1.PodFailed, 3*time.Minute))
	h.finishJob(batchv1.JobFailed, 1)
	h.expect(h.reconcile(), flywayv1alpha1.ReasonRetryLimitExceeded, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	expectVersion("failed")

	// pods of the earlier Jobs are not garbage collected here, so each later pod is younger
	for i, pod := range []string{"g3", "g4"} {
		h.updateImage("somereg.io/someimage:x", int64(3+i))
		h.reconcile()
		h.reconcile()
		h.addPod(pod, ended(corev1.PodSucceeded, time.Duration(2-i)*time.Minute))
		h.finishJob(batchv1.JobComplete, 0)
		h.expect(h.reconcile(), flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
		expectVersion(pod)
	}
}

// A log that cannot be read changes nothing: the result, events and requeue are those of a run without a reader.
func TestSchemaVersionReadErrorChangesNothing(t *testing.T) {
	forbidden := apierrors.NewForbidden(schema.GroupResource{Resource: "pods/log"}, "p", nil)
	for name, setup := range map[string]func(h *statusHarness){
		"no reader":       func(h *statusHarness) { h.r.LogReader = nil },
		"forbidden":       func(h *statusHarness) { withLogs(h, nil).err = forbidden },
		"pod log missing": func(h *statusHarness) { withLogs(h, map[string]string{}) },
		"no json":         func(h *statusHarness) { withLogs(h, map[string]string{"p": "text-output.log"}) },
		"pods forbidden":  func(h *statusHarness) { withLogs(h, map[string]string{"p": "default-run.log"}) },
	} {
		t.Run(name, func(t *testing.T) {
			var rvs []string
			h := newMetricsHarness(t, nil, podLists(name == "pods forbidden", &rvs))
			setup(h)
			h.reconcile()
			if name != "pods forbidden" {
				h.addPod("p", ended(corev1.PodSucceeded, time.Minute))
			}
			h.finishJob(batchv1.JobComplete, 0)
			h.events()
			res := h.reconcileResult()
			m := h.migration()
			h.expect(m, flywayv1alpha1.ReasonJobSucceeded, metav1.ConditionTrue, metav1.ConditionFalse, metav1.ConditionFalse)
			if res.RequeueAfter != 0 || m.Status.SchemaVersion != "" {
				t.Errorf("requeue %v, schemaVersion %q; want none", res.RequeueAfter, m.Status.SchemaVersion)
			}
			if events := h.events(); len(events) != 1 || !strings.HasPrefix(events[0], "Normal Succeeded") {
				t.Errorf("events = %v, want only Succeeded", events)
			}
		})
	}
}

// The newest pod is read: with backoffLimit 2 a Job can have failed pods before the one that succeeded, and on a
// deadline the newest may still be terminating, when no older pod's output is attached.
func TestFlywayOutputReadsTheNewestPod(t *testing.T) {
	h := newMetricsHarness(t, nil)
	logs := withLogs(h, map[string]string{"old": "migrate-error.log", "new": "default-run.log"})
	h.reconcile()
	h.addPod("old", ended(corev1.PodFailed, 2*time.Minute))
	h.addPod("new", ended(corev1.PodSucceeded, time.Minute))
	h.finishJob(batchv1.JobComplete, 1)
	if m := h.reconcile(); m.Status.SchemaVersion != "2" || len(logs.calls) != 1 || logs.calls[0] != "new" {
		t.Errorf("schemaVersion %q from %v, want 2 from the newest pod", m.Status.SchemaVersion, logs.calls)
	}

	h = newMetricsHarness(t, ptr.To[int32](0))
	logs = withLogs(h, map[string]string{"old": "migrate-error.log", "new": "migrate-error.log"})
	h.reconcile()
	h.addPod("old", ended(corev1.PodFailed, 2*time.Minute))
	h.addPod("new", ended(corev1.PodRunning, time.Minute))
	h.failJobDeadlineExceeded()
	h.expect(h.reconcile(), flywayv1alpha1.ReasonTimedOut, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	if msg := h.message(); strings.Contains(msg, flywayErrorSeparator) || len(logs.calls) != 0 {
		t.Errorf("message %q after reads %v: an older pod's error was attached", msg, logs.calls)
	}
}

func TestFlywayErrorInRetryingMessage(t *testing.T) {
	h := newMetricsHarness(t, nil)
	logs := withLogs(h, map[string]string{"p": "migrate-error.log"})
	h.reconcile()
	h.reconcile()
	h.addPod("p", ended(corev1.PodFailed, time.Minute))
	h.holdJob()
	h.finishJob(batchv1.JobFailed, 3)

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonRetrying, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	want := "Job status-migration failed on attempt 1 of 3, retrying: BackoffLimitExceeded: Job has reached the specified backoff limit" +
		flywayErrorSeparator + "Failed to execute script V3__bad.sql SQL State : null"
	msg := h.message()
	if !strings.HasPrefix(msg, want) {
		t.Fatalf("message = %q, want prefix %q", msg, want)
	}

	// the Job terminates: the message stays, the log is not read again, and the status is not written
	writes := h.statusUpdates
	for range 3 {
		h.reconcile()
	}
	if h.message() != msg || len(logs.calls) != 1 || h.statusUpdates != writes {
		t.Errorf("while terminating: message %q, reads %v, writes %d; want unchanged, 1, 0", h.message(), logs.calls, h.statusUpdates-writes)
	}
	h.expectRuns(0, 1, 0)

	// the next attempt reads its own pod
	h.releaseJob()
	h.reconcile()
	h.reconcile()
	h.addPod("p2", ended(corev1.PodFailed, 0))
	logs.logs["p2"] = "conn-error.log"
	h.finishJob(batchv1.JobFailed, 3)
	h.reconcile()
	if msg := h.message(); !strings.Contains(msg, "attempt 2 of 3") || !strings.Contains(msg, "Unable to obtain connection") {
		t.Errorf("attempt 2 message = %q", msg)
	}
}

func TestFlywayErrorInFailedMessageAndEvent(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	logs := withLogs(h, map[string]string{"p": "migrate-error.log"})
	h.reconcile()
	h.addPod("p", ended(corev1.PodFailed, time.Minute))
	h.finishJob(batchv1.JobFailed, 3)
	h.events()

	m := h.reconcile()
	h.expect(m, flywayv1alpha1.ReasonRetryLimitExceeded, metav1.ConditionFalse, metav1.ConditionFalse, metav1.ConditionTrue)
	msg := meta.FindStatusCondition(m.Status.Conditions, flywayv1alpha1.ConditionFailed).Message
	if !strings.HasPrefix(msg, "Job status-migration failed on attempt 1 of 1: BackoffLimitExceeded: Job has reached the specified backoff limit"+
		flywayErrorSeparator+"Failed to execute script V3__bad.sql") {
		t.Errorf("Failed message = %q", msg)
	}
	if n := countContaining(h.events(), "Warning Failed Migration failed on attempt 1 of 1: BackoffLimitExceeded: Job has reached the specified backoff limit"+
		flywayErrorSeparator+"Failed to execute script V3__bad.sql"); n != 1 {
		t.Errorf("Failed events with the Flyway error = %d, want 1", n)
	}

	writes := h.statusUpdates
	for range 3 {
		h.reconcile()
	}
	if len(logs.calls) != 1 || h.statusUpdates != writes {
		t.Errorf("after the failure was recorded: reads %v, writes %d; want 1, 0", logs.calls, h.statusUpdates-writes)
	}
	h.expectRuns(0, 0, 1)
}

// A failure whose log cannot be read keeps the plain message, and later reconciles do not retry the read.
func TestFlywayErrorUnreadableKeepsPlainMessage(t *testing.T) {
	h := newMetricsHarness(t, ptr.To[int32](0))
	logs := withLogs(h, map[string]string{"p": "text-output.log"})
	h.reconcile()
	h.addPod("p", ended(corev1.PodFailed, time.Minute))
	h.finishJob(batchv1.JobFailed, 3)
	h.reconcile()
	want := "Job status-migration failed on attempt 1 of 1: BackoffLimitExceeded: Job has reached the specified backoff limit"
	h.reconcile()
	if msg := h.message(); msg != want || len(logs.calls) != 1 {
		t.Errorf("message %q after reads %v, want %q after one read", msg, logs.calls, want)
	}
}

// An older generation's Job that succeeds late sets schemaVersion in the write that records lastApplied*, before
// the Job is deleted.
func TestSchemaVersionFromPreviousGenerationJob(t *testing.T) {
	h := newStatusHarness(t, nil)
	withLogs(h, map[string]string{"p": "first-migrate.log"})
	h.reconcile()
	h.addPod("p", running)
	h.updateImage("somereg.io/someimage:2", 2)
	h.reconcile() // previous job running
	p := &corev1.Pod{}
	if err := h.c.Get(context.TODO(), types.NamespacedName{Namespace: statusKey.Namespace, Name: "p"}, p); err != nil {
		t.Fatal(err)
	}
	p.Status.Phase = corev1.PodSucceeded
	if err := h.c.Status().Update(context.TODO(), p); err != nil {
		t.Fatal(err)
	}
	h.finishJob(batchv1.JobComplete, 0)
	writes := len(h.statusWrites)

	h.expect(h.reconcile(), flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	if len(h.statusWrites) == writes {
		t.Fatal("no status write")
	}
	first := h.statusWrites[writes]
	if first.SchemaVersion != "2" || first.LastAppliedGeneration != 1 {
		t.Errorf("first write: schemaVersion %q, lastAppliedGeneration %d; want 2 and 1 together", first.SchemaVersion, first.LastAppliedGeneration)
	}
}
