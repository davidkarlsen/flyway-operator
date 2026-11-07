-- Outlasts spec.activeDeadline, so the Job hits its activeDeadlineSeconds.
SELECT pg_sleep(600);
