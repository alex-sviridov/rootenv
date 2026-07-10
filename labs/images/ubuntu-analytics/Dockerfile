FROM ubuntu:22.04

RUN apt-get update -y && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        sqlite3 \
        less \
        vim \
        curl \
        ca-certificates \
        gzip \
        procps \
    && rm -rf /var/lib/apt/lists/*

# Non-root student user — this is the container's default user, so both the
# lab setup script (run as the pod's PID-1 command) and later exec sessions
# (relay's kubectl exec has no per-session --user override) land as `lab`.
RUN useradd -m -s /bin/bash lab

# Service account used by lab content to demonstrate per-service ownership.
RUN useradd -m -s /bin/bash datastream

# Seeds the DataStream demo scenario (app code, config, logs, junk files) at
# build time — the runtime setup script has no root access to do this itself.
COPY seed.sh /tmp/seed.sh
RUN bash /tmp/seed.sh && rm /tmp/seed.sh

USER lab
WORKDIR /home/lab
