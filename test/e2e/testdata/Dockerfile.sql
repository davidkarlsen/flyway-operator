# Migration-source image: the operator's init container runs `cp` from it, so the base must ship a shell and cp.
FROM docker.io/library/busybox:1.37
COPY *.sql /sql/
