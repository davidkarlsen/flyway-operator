package controller

import (
	hashutil "github.com/openyurtio/openyurt/pkg/controller/yurtstaticset/util"
	batchv1 "k8s.io/api/batch/v1"
	v12 "k8s.io/api/core/v1"
)

func jobsAreEqual(first *batchv1.Job, second *batchv1.Job) bool {
	return first != nil && second != nil && first.Annotations[podTemplateHashAnnotation] == second.Annotations[podTemplateHashAnnotation]
}

// from https://github.com/kubernetes/kubernetes/blob/v1.28.1/pkg/controller/job/utils.go
// IsJobFinished checks whether the given Job has finished execution.
// It does not discriminate between successful and failed terminations.
func isJobFinished(j *batchv1.Job) bool {
	for _, c := range j.Status.Conditions {
		if (c.Type == batchv1.JobComplete || c.Type == batchv1.JobFailed) && c.Status == v12.ConditionTrue {
			return true
		}
	}
	return false
}

func addPodSpecHash(job *batchv1.Job) *batchv1.Job {
	hash := hashutil.ComputeHash(&job.Spec.Template)
	job.Annotations[podTemplateHashAnnotation] = hash

	return job
}
