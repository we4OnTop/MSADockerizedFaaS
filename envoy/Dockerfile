FROM envoyproxy/envoy:v1.30.1

# 2. (Optional) Set the log level to verify startup
# The base image already has an ENTRYPOINT that runs envoy
CMD ["/usr/local/bin/envoy", "-c", "/etc/envoy/envoy.yaml", "--component-log-level", "upstream:debug,connection:trace"]