FROM centos:7 AS build

RUN yum -y update --disablerepo=extras && yum -y install device-mapper findutils zfs build-base linux-headers bash git wget make gcc

RUN wget https://golang.org/dl/go1.14.4.linux-amd64.tar.gz && \
  tar -C /usr/local -xzf go1.14.4.linux-amd64.tar.gz && \
  rm -f go1.14.4.linux-amd64.tar.gz

ENV PATH=$PATH::/usr/local/go/bin

RUN wget https://sourceforge.net/projects/perfmon2/files/libpfm4/libpfm-4.10.1.tar.gz && \
  echo "78599e1668142f48c24afb1f79ca00a89df51b65  libpfm-4.10.1.tar.gz" | sha1sum -c  && \
  tar -xzf libpfm-4.10.1.tar.gz && \
  rm libpfm-4.10.1.tar.gz

RUN export DBG="-g -Wall" && \
  make -e -C libpfm-4.10.1 && \
  make install -C libpfm-4.10.1

RUN cd /etc/yum.repos.d/ && \
  wget https://copr.fedorainfracloud.org/coprs/jhli/ipmctl/repo/epel-7/jhli-ipmctl-epel-7.repo && \
  wget https://copr.fedorainfracloud.org/coprs/jhli/safeclib/repo/epel-7/jhli-safeclib-epel-7.repo && \
  yum install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-7.noarch.rpm && \
  yum install --enablerepo="rhel-*-optional-rpms,rhel-*-extras-rpms,rhel-ha-for-rhel-*-server-rpms" -y ndctl ndctl-libs ndctl-devel libsafec rubygem-asciidoctor && \
  yum install -y ipmctl ipmctl-devel


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

ENV CADVISOR_HEALTHCHECK_URL=http://localhost:8080/healthz

HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget --quiet --tries=1 --spider $CADVISOR_HEALTHCHECK_URL || exit 1

ENTRYPOINT ["/usr/bin/cadvisor", "-logtostderr", "--port=9101"]
