FROM ubuntu:22.04

RUN apt-get update -y && \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        openssh-server \
        openssh-client \
        ca-certificates \
        python3 \
        sudo \
        vim \
        less \
        curl \
        wget \
        iputils-ping \
        iproute2 \
        net-tools \
        dnsutils \
        tcpdump \
        netcat-openbsd \
        rsync \
        procps \
        psmisc \
        lsof \
    && rm -rf /var/lib/apt/lists/* && \
    mkdir -p /run/sshd && \
    chmod u+s /usr/bin/ping

COPY sshd_config /etc/ssh/sshd_config
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 22

ENTRYPOINT ["/entrypoint.sh"]
