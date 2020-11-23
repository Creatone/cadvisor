FROM centos:7 AS build

RUN yum -y update --disablerepo=extras && yum -y install device-mapper findutils zfs build-base linux-headers bash git wget make gcc

RUN yum -y groupinstall 'Development Tools'


RUN wget -q https://golang.org/dl/go1.14.4.linux-amd64.tar.gz && \
  tar -C /usr/local -xzf go1.14.4.linux-amd64.tar.gz && \
  rm -f go1.14.4.linux-amd64.tar.gz

ENV PATH=$PATH::/usr/local/go/bin

RUN wget -q https://sourceforge.net/projects/perfmon2/files/libpfm4/libpfm-4.10.1.tar.gz && \
  tar -xzf libpfm-4.10.1.tar.gz && \
  rm libpfm-4.10.1.tar.gz

RUN wget -q https://github.com/intel/ipmctl/archive/v02.00.00.3719.tar.gz && \
    tar -xzf v02.00.00.3719.tar.gz && \
    rm v02.00.00.3719.tar.gz

RUN export DBG="-g -Wall" && \
  make -e -C libpfm-4.10.1 && \
  make install -C libpfm-4.10.1

RUN cd /etc/yum.repos.d/ && \
  wget -q https://copr.fedorainfracloud.org/coprs/jhli/ipmctl/repo/epel-7/jhli-ipmctl-epel-7.repo && \
  wget -q https://copr.fedorainfracloud.org/coprs/jhli/safeclib/repo/epel-7/jhli-safeclib-epel-7.repo && \
  yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm && \
  yum install --enablerepo="rhel-*-optional-rpms,rhel-*-extras-rpms,rhel-ha-for-rhel-*-server-rpms" -y ndctl ndctl-libs ndctl-devel libsafec rubygem-asciidoctor

RUN yum -y install cmake

RUN cd ipmctl-02.00.00.3719 && \
    mkdir output && cd output && \
    cmake -DRELEASE=ON -DCMAKE_INSTALL_PREFIX=/ .. && \
    make -j all && \
    make install



ADD . /go/src/github.com/google/cadvisor
WORKDIR /go/src/github.com/google/cadvisor

ENV GOROOT /usr/local/go/
ENV GOPATH /go
ENV GO_FLAGS="-tags=libpfm,ipmctl,netgo"
RUN ./build/build.sh


FROM alpine:3.12
MAINTAINER dengnan@google.com vmarmol@google.com vishnuk@google.com jimmidyson@gmail.com stclair@google.com

RUN apk --no-cache add libc6-compat device-mapper findutils zfs && \
    apk --no-cache add thin-provisioning-tools --repository http://dl-3.alpinelinux.org/alpine/edge/main/ && \
    echo 'hosts: files mdns4_minimal [NOTFOUND=return] dns mdns4' >> /etc/nsswitch.conf && \
    rm -rf /var/cache/apk/*

# Grab cadvisor and libpfm4 from "build" container.
COPY --from=build /usr/local/lib/libpfm.so* /usr/local/lib/
COPY --from=build /go/src/github.com/google/cadvisor/cadvisor /usr/bin/cadvisor
COPY --from=build /usr/share/ipmctl /usr/share/ipmctl

EXPOSE 9101

ENV CADVISOR_HEALTHCHECK_URL=http://localhost:9101/healthz

HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget --quiet --tries=1 --spider $CADVISOR_HEALTHCHECK_URL || exit 1

ENTRYPOINT ["/usr/bin/cadvisor", "-logtostderr", "--port=9101"]
