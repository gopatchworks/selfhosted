# Run: ruby tests/kubefaas_builder_readiness_test.rb (Helm required; no cluster access).
#
# The KubeFaaS builder builds every template's base image after it starts, one
# at a time. Its /v1/readyz fails until they are all in the registry, while
# /v1/healthz only says the process is up. Readiness must use /v1/readyz, as the
# KubeFaaS chart does, or the Service sends function builds to a builder whose
# base images are missing and they fail ("failed to resolve source metadata").
require 'yaml'
require 'open3'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def check(expected, actual, what)
  raise "#{what}: expected #{expected.inspect}, got #{actual.inspect}" unless expected == actual
end
output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'patchworks', '--set', 'kubefaas.enabled=true')
raise error unless status.success?
builder = YAML.load_stream(output).compact.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name') == 'patchworks-kubefaas-builder' }
container = builder.dig('spec', 'template', 'spec', 'containers').find { |c| c['name'] == 'kubefaas-builder' }
check('/v1/readyz', container.dig('readinessProbe', 'httpGet', 'path'), 'builder readiness path')
check('/v1/healthz', container.dig('livenessProbe', 'httpGet', 'path'), 'builder liveness path')
check('/v1/healthz', container.dig('startupProbe', 'httpGet', 'path'), 'builder startup path')
puts 'kubefaas_builder_readiness_test.rb: ok'
