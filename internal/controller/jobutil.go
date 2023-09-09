package controller

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	eq "k8s.io/apimachinery/pkg/api/equality"
)

func jobsAreEqual(first *batchv1.Job, second *batchv1.Job) bool {
	return first != nil && second != nil && eq.Semantic.DeepEqual(first.Spec.Template, second.Spec.Template)
}

// from https://github.com/kubernetes/kubernetes/blob/v1.28.1/pkg/controller/job/utils.go
// IsJobFinished checks whether the given Job has finished execution.
// It does not discriminate between successful and failed terminations.
func isJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func hasFailed(job *batchv1.Job) bool {
	return job.Status.Failed > 0
}

func hasSucceeded(job *batchv1.Job) bool {
	return job.Status.Succeeded > 0
}
