FROM scratch
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/ipgeo /usr/bin/ipgeo
ENTRYPOINT ["/usr/bin/ipgeo"]
