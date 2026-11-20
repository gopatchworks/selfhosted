# Run: ruby tests/elasticsearch_sysctl_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'

def assert(value, message)
  raise message unless value
end

def elasticsearch_pod(values = {})
  Tempfile.create(['elasticsearch-sysctl', '.yaml']) do |file|
    file.write(YAML.dump(values))
    file.flush
    # Helm rejects the "--- {}" YAML.dump writes for an empty hash.
    values_args = values.empty? ? [] : ['-f', file.path]
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', File.expand_path('../charts/patchworks-infra', __dir__),
      '--namespace', 'install', '--show-only', 'templates/elasticsearch.yaml', *values_args
    )
    raise error unless status.success?

    deployment = YAML.load_stream(output).compact.find { |document| document['kind'] == 'Deployment' }
    assert(deployment, 'Elasticsearch Deployment did not render')
    deployment.dig('spec', 'template', 'spec')
  end
end

def privileged?(container)
  container.dig('securityContext', 'privileged') == true
end

sysctl = {
  'name' => 'sysctl',
  'image' => 'busybox:1.37',
  'securityContext' => {'privileged' => true},
  'command' => ['sysctl', '-w', 'vm.max_map_count=262144']
}

# Off by default: nothing privileged, so Pod Security "baseline" admits the pod.
[{}, {'elasticsearch' => {'sysctlInitContainer' => nil}}].each do |values|
  pod = elasticsearch_pod(values)
  assert(!pod.key?('initContainers'), "#{values.inspect}: rendered initContainers #{pod['initContainers'].inspect}")
  assert(pod['containers'].none? { |container| privileged?(container) },
         "#{values.inspect}: rendered a privileged container")
end

# Turned on, the init container renders as it did before the flag existed.
pod = elasticsearch_pod('elasticsearch' => {'sysctlInitContainer' => {'enabled' => true}})
assert(pod['initContainers'] == [sysctl], "enabled: expected #{[sysctl].inspect}, got #{pod['initContainers'].inspect}")

puts 'Elasticsearch sysctl init container render tests passed'
