# Migration-source image: mounted read-only as an image volume, so it needs no shell or other tools.
FROM scratch
COPY *.sql /sql/
