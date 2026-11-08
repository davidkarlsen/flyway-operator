package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	flywayv1alpha1 "github.com/davidkarlsen/flyway-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// addPod creates a pod the Job controller would have made for the Job; mutate shapes its status.
func (h *statusHarness) addPod(name string, mutate func(p *corev1.Pod)) {
	h.t.Helper()
	j := h.job()
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: j.Namespace,
			Labels:          map[string]string{jobNameLabel: j.Name},
			OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(j, schemaJob)},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}
	if mutate != nil {
		mutate(p)
	}
	status := p.Status
	if err := h.c.Create(context.TODO(), p); err != nil {
		h.t.Fatal(err)
	}
	p.Status = status
	if err := h.c.Update(context.TODO(), p); err != nil {
		h.t.Fatal(err)
	}
}

var schemaJob = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}

func waiting(reason, message string) func(p *corev1.Pod) {
	return func(p *corev1.Pod) {
		p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: flywayContainerName,
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: reason, Message: message}}}}
	}
}

func unschedulable(message string) func(p *corev1.Pod) {
	return func(p *corev1.Pod) {
		p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: message}}
	}
}

func running(p *corev1.Pod) {
	p.Status.Phase = corev1.PodRunning
	p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: flywayContainerName, Started: ptr.To(true),
		State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: metav1.Now()}}}}
}

func (h *statusHarness) addFailedCreate(message string) {
	h.t.Helper()
	j := h.job()
	e := &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "ev-" + message[:3], Namespace: j.Namespace},
		InvolvedObject: corev1.ObjectReference{Kind: "Job", Name: j.Name, UID: j.UID, Namespace: j.Namespace},
		Reason:         "FailedCreate", Message: message, LastTimestamp: metav1.Now(),
	}
	if err := h.c.Create(context.TODO(), e); err != nil {
		h.t.Fatal(err)
	}
}

func (h *statusHarness) progressing() *metav1.Condition {
	return meta.FindStatusCondition(h.migration().Status.Conditions, flywayv1alpha1.ConditionProgressing)
}

func (h *statusHarness) reconcileResult() reconcile.Result {
	h.t.Helper()
	res, err := h.r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: statusKey})
	if err != nil {
		h.t.Fatal(err)
	}
	return res
}

// runningJob returns a harness with the generation 1 Job created and its first status written.
func runningJob(t *testing.T, extra ...interceptor.Funcs) *statusHarness {
	h := newMetricsHarness(t, nil, extra...)
	h.reconcile()
	// a Job of some age, so that "no pod yet" is a finding
	j := h.job()
	j.CreationTimestamp = metav1.NewTime(time.Now().Add(-5 * time.Minute))
	if err := h.c.Update(context.TODO(), j); err != nil {
		t.Fatal(err)
	}
	return h
}

func TestPodWaitMessages(t *testing.T) {
	long := strings.Repeat("x", 1000)
	for _, tt := range []struct {
		name  string
		setup func(h *statusHarness)
		want  string // substring of the Progressing message; "" means the plain running message
	}{
		{"ImagePullBackOff", func(h *statusHarness) {
			h.addPod("p", waiting("ImagePullBackOff", `Back-off pulling image "reg/flyway:x"`))
		}, `image pull failed: flyway: Back-off pulling image "reg/flyway:x"`},
		{"ErrImagePull", func(h *statusHarness) {
			h.addPod("p", waiting("ErrImagePull", "manifest unknown"))
		}, "image pull failed: flyway: manifest unknown"},
		{"CreateContainerConfigError", func(h *statusHarness) {
			h.addPod("p", waiting("CreateContainerConfigError", `secret "pw" not found`))
		}, `container cannot start: flyway: CreateContainerConfigError: secret "pw" not found`},
		{"CreateContainerError", func(h *statusHarness) {
			h.addPod("p", waiting("CreateContainerError", "boom"))
		}, "container cannot start: flyway: CreateContainerError: boom"},
		{"unschedulable", func(h *statusHarness) {
			h.addPod("p", unschedulable("0/3 nodes are available: 3 Insufficient cpu."))
		}, "pod unschedulable: 0/3 nodes are available: 3 Insufficient cpu."},
		{"FailedCreate event", func(h *statusHarness) {
			h.addFailedCreate(`pods "x" is forbidden: exceeded quota: q, requested: limits.cpu=1`)
		}, `pod not created: pods "x" is forbidden: exceeded quota: q`},
		{"no pod and no event", func(h *statusHarness) {}, "no pod created after 5m0s, see `kubectl describe job status-migration`"},
		{"ContainerCreating is not a finding", func(h *statusHarness) {
			h.addPod("p", waiting("ContainerCreating", ""))
		}, ""},
		{"started container", func(h *statusHarness) { h.addPod("p", running) }, ""},
		{"pod of another owner is ignored", func(h *statusHarness) {
			h.addPod("p", func(p *corev1.Pod) {
				waiting("ImagePullBackOff", "other")(p)
				p.OwnerReferences[0].UID = "someone-else"
			})
		}, "no pod created after"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := runningJob(t)
			tt.setup(h)
			h.reconcileResult()
			c := h.progressing()
			if c.Reason != flywayv1alpha1.ReasonJobRunning {
				t.Errorf("reason = %s, want JobRunning", c.Reason)
			}
			if !strings.HasPrefix(c.Message, "Job status-migration attempt 1 of 3 is running") {
				t.Errorf("message lost the attempt text: %q", c.Message)
			}
			if tt.want == "" && c.Message != "Job status-migration attempt 1 of 3 is running" {
				t.Errorf("message = %q, want the plain one", c.Message)
			}
			if tt.want != "" && !strings.Contains(c.Message, tt.want) {
				t.Errorf("message = %q, want it to contain %q", c.Message, tt.want)
			}
		})
	}

	t.Run("long messages are truncated", func(t *testing.T) {
		h := runningJob(t)
		h.addPod("p", waiting("ErrImagePull", long))
		h.reconcileResult()
		c := h.progressing()
		if n := len([]rune(c.Message)); n > 450 || !strings.HasSuffix(c.Message, "…") {
			t.Errorf("message has %d runes, want a truncated one: %q", n, c.Message)
		}
	})
}

func TestPodWaitRequeuesOnlyWhileNothingStarted(t *testing.T) {
	h := runningJob(t)
	if res := h.reconcileResult(); res.RequeueAfter != podWaitRequeue {
		t.Errorf("no pod: requeueAfter = %s, want %s", res.RequeueAfter, podWaitRequeue)
	}
	h.addPod("p", running)
	if res := h.reconcileResult(); res.RequeueAfter != 0 {
		t.Errorf("started pod: requeueAfter = %s, want none", res.RequeueAfter)
	}
}

func TestPodWaitWritesStatusOnlyWhenTheMessageChanges(t *testing.T) {
	h := runningJob(t)
	h.addPod("p", waiting("ErrImagePull", "manifest unknown"))
	h.reconcileResult()
	writes := h.statusUpdates
	for range 3 {
		h.reconcileResult()
	}
	if h.statusUpdates != writes {
		t.Errorf("status writes grew from %d to %d for an unchanged message", writes, h.statusUpdates)
	}
}

func TestPodWaitKeepsPullCauseWhileBackingOff(t *testing.T) {
	h := runningJob(t)
	h.addPod("p", waiting("ErrImagePull", "manifest unknown"))
	h.reconcileResult()
	want := h.progressing().Message

	h.addPod("p2", nil) // an unrelated pending pod must not matter
	p := &corev1.Pod{}
	if err := h.c.Get(context.TODO(), client.ObjectKey{Namespace: "ns", Name: "p"}, p); err != nil {
		t.Fatal(err)
	}
	waiting("ImagePullBackOff", `Back-off pulling image "reg/flyway:x"`)(p)
	if err := h.c.Update(context.TODO(), p); err != nil {
		t.Fatal(err)
	}
	h.reconcileResult()
	if got := h.progressing().Message; got != want {
		t.Errorf("message = %q, want the ErrImagePull cause kept: %q", got, want)
	}
}

func TestPodWaitFallsBackWhenEventsAreForbidden(t *testing.T) {
	h := runningJob(t, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
		if _, ok := list.(*corev1.EventList); ok {
			return apierrors.NewForbidden(schema.GroupResource{Resource: "events"}, "", errors.New("no rbac"))
		}
		return c.List(ctx, list, opts...)
	}})
	h.addFailedCreate("exceeded quota")
	h.reconcileResult()
	if m := h.progressing().Message; !strings.Contains(m, "no pod created after") || strings.Contains(m, "quota") {
		t.Errorf("message = %q, want the age fallback", m)
	}
}

// supersede bumps the spec to generation 2, so the existing Job is of an older generation.
func (h *statusHarness) supersede() {
	h.t.Helper()
	m := h.migration()
	m.Spec.MigrationSource.ImageRef = "somereg.io/someimage:2"
	m.Generation = 2
	if err := h.c.Update(context.TODO(), m); err != nil {
		h.t.Fatal(err)
	}
}

func TestPodWaitReplacesOldJobThatNeverStarted(t *testing.T) {
	copySQLDone := func(p *corev1.Pod) {
		p.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: copySQLContainerName,
			State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}}
	}
	for _, tt := range []struct {
		name  string
		setup func(h *statusHarness)
	}{
		{"no pod (FailedCreate)", func(h *statusHarness) { h.addFailedCreate("exceeded quota") }},
		{"ImagePullBackOff", func(h *statusHarness) { h.addPod("p", waiting("ImagePullBackOff", "x")) }},
		{"ImagePullBackOff after a finished copy-sql init container", func(h *statusHarness) {
			h.addPod("p", func(p *corev1.Pod) { copySQLDone(p); waiting("ErrImagePull", "x")(p) })
		}},
		{"unschedulable", func(h *statusHarness) { h.addPod("p", unschedulable("0/3 nodes")) }},
		{"ContainerCreating", func(h *statusHarness) { h.addPod("p", waiting("ContainerCreating", "")) }},
		{"a running pod of another owner", func(h *statusHarness) {
			h.addPod("p", func(p *corev1.Pod) { running(p); p.OwnerReferences[0].UID = "someone-else" })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := runningJob(t)
			tt.setup(h)
			h.reconcile()
			h.supersede()

			m := h.reconcile()
			h.expect(m, flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
			if !strings.Contains(m.Status.Conditions[0].Message, "none of its containers ever started") {
				t.Errorf("message = %q", m.Status.Conditions[0].Message)
			}
			if h.job() != nil {
				t.Fatal("never-started job of the previous generation was not deleted")
			}
			h.expectJobDeletes(1)
			if h.lastJobDeletePropagation != metav1.DeletePropagationForeground {
				t.Errorf("propagation = %q, want Foreground so the successor waits for the old pods", h.lastJobDeletePropagation)
			}
			if countContaining(h.events(), "none of its containers ever started") != 1 {
				t.Error("want exactly one replacement event")
			}
			h.expectRuns(0, 0, 0) // nothing ran: no outcome

			m = h.reconcile()
			h.expect(m, flywayv1alpha1.ReasonJobCreated, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
			if j := h.job(); j == nil || j.Annotations[flywayv1alpha1.Generation] != "2" {
				t.Fatalf("generation 2 job was not created: %v", j)
			}
			h.expectRuns(0, 0, 0)
		})
	}
}

// The safety rule: a Job whose Flyway container started, or might have, is waited for, never replaced.
func TestPodWaitDoesNotReplaceOldJobThatStarted(t *testing.T) {
	for _, tt := range []struct {
		name  string
		setup func(h *statusHarness)
	}{
		{"running container", func(h *statusHarness) { h.addPod("p", running) }},
		{"running container next to a stuck pod", func(h *statusHarness) {
			h.addPod("p", waiting("ImagePullBackOff", "x"))
			h.addPod("q", running)
		}},
		{"container started but not running any more", func(h *statusHarness) {
			h.addPod("p", func(p *corev1.Pod) {
				p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: flywayContainerName,
					State:                corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
					LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 1}}}}
			})
		}},
		{"terminated container", func(h *statusHarness) {
			h.addPod("p", func(p *corev1.Pod) {
				p.Status.ContainerStatuses = []corev1.ContainerStatus{{Name: flywayContainerName,
					State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0}}}}
			})
		}},
		{"pod past Pending", func(h *statusHarness) { h.addPod("p", func(p *corev1.Pod) { p.Status.Phase = corev1.PodFailed }) }},
		{"a failed pod was cleaned up, a retry is Pending", func(h *statusHarness) {
			h.addPod("p", waiting("ContainerCreating", ""))
			j := h.job()
			j.Status.Failed = 1
			if err := h.c.Status().Update(context.TODO(), j); err != nil {
				h.t.Fatal(err)
			}
		}},
		{"the Job counts an active pod that is not listed", func(h *statusHarness) {
			j := h.job()
			j.Status.Active = 1
			if err := h.c.Status().Update(context.TODO(), j); err != nil {
				h.t.Fatal(err)
			}
		}},
		{"terminating pod counted by the Job", func(h *statusHarness) {
			j := h.job()
			j.Status.Terminating = ptr.To[int32](1)
			if err := h.c.Status().Update(context.TODO(), j); err != nil {
				h.t.Fatal(err)
			}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := runningJob(t)
			tt.setup(h)
			h.reconcile()
			h.supersede()

			m := h.reconcile()
			h.expect(m, flywayv1alpha1.ReasonPreviousJobRunning, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
			if h.job() == nil {
				t.Fatal("job whose container started was deleted")
			}
			h.expectJobDeletes(0)
		})
	}

	t.Run("pods cannot be listed", func(t *testing.T) {
		fail := false
		h := runningJob(t, interceptor.Funcs{List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if _, ok := list.(*corev1.PodList); ok && fail {
				return apierrors.NewForbidden(schema.GroupResource{Resource: "pods"}, "", errors.New("no rbac"))
			}
			return c.List(ctx, list, opts...)
		}})
		h.supersede()
		fail = true
		h.expect(h.reconcile(), flywayv1alpha1.ReasonPreviousJobRunning, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
		if h.job() == nil {
			t.Fatal("job was deleted although its pods are unknown")
		}
		h.expectJobDeletes(0)
	})
}

func TestPodWaitReplacementSurvivesTerminatingJob(t *testing.T) {
	h := runningJob(t)
	h.addPod("p", waiting("ErrImagePull", "x"))
	h.reconcile()
	h.holdJob() // the foreground deletion keeps the Job until its pods are gone
	h.supersede()
	for range 3 {
		h.expect(h.reconcile(), flywayv1alpha1.ReasonPreviousJobReplaced, metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionFalse)
	}
	if c := h.progressing(); !strings.Contains(c.Message, "none of its containers ever started") {
		t.Errorf("message flipped while terminating: %q", c.Message)
	}
	h.expectJobDeletes(1)
	if h.job() == nil || h.job().Annotations[flywayv1alpha1.Generation] != "1" {
		t.Fatal("successor created while the old Job still exists")
	}
	h.expectRuns(0, 0, 0)
}
