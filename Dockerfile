FROM envoyproxy/envoy:v1.36.2

COPY ./envoy/envoy.yaml /etc/envoy/envoy.yaml

EXPOSE 9901 10000

CMD ["/usr/local/bin/envoy", "-c", "/etc/envoy/envoy.yaml", "--service-cluster", "envoy", "--log-level", "debug"]
