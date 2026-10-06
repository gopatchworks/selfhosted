# Run: ruby tests/mono_bucket_creation_test.rb (Helm only; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)
IN_CLUSTER = 'http://patchworks-s3-manager.install.svc.cluster.local:8080'

def assert(value, message)
  raise message unless value
end

def render(values = {})
  values = { 'workers' => { 'type' => 'mono', 'companies' => [{ 'name' => 'acme' }] } }.merge(values)
  Tempfile.create(['mono-bucket-creation', '.yaml']) do |file|
    file.write(JSON.generate(values))
    file.flush
    output, stderr, status = Open3.capture3('helm', 'template', 'test', CHART,
      '--namespace', 'install', '--set', 'credentials.autoGenerate=false',
      '--set', 'pusher.existingSecret.name=fixture-pusher',
      '--set', 'app.existingSecret.name=fixture-app',
      '--set', 'passport.existingSecret.name=fixture-passport', '-f', file.path)
    raise stderr unless status.success?
    YAML.load_stream(output).compact
  end
end

# The bucket creation endpoint each mono worker Deployment receives, by name.
def mono_endpoints(documents)
  pods = documents.select do |document|
    document['kind'] == 'Deployment' &&
      document.dig('metadata', 'labels', 'app.kubernetes.io/component').to_s.start_with?('workers')
  end
  assert(pods.map { |pod| pod.dig('metadata', 'name') }.sort == %w[patchworks-workers patchworks-workers-acme],
         "Unexpected worker Deployments: #{pods.map { |pod| pod.dig('metadata', 'name') }}")
  pods.to_h do |pod|
    env = pod.dig('spec', 'template', 'spec', 'containers', 0, 'env') || []
    [pod.dig('metadata', 'name'), env.find { |entry| entry['name'] == 'S3_BUCKET_CREATION_ENDPOINT' }&.fetch('value')]
  end
end

def expect_endpoints(values, expected, description)
  mono_endpoints(render(values)).each do |name, endpoint|
    assert(endpoint == expected, "#{description}: #{name} got #{endpoint.inspect}, want #{expected.inspect}")
  end
end

# By default the hub and company workers use the in-cluster S3 Manager, as Core does.
expect_endpoints({}, IN_CLUSTER, 'default')

expect_endpoints({ 's3Manager' => { 'external' => { 'endpoint' => 'https://s3-manager.example.com' } } },
                 'https://s3-manager.example.com', 'external S3 Manager')

expect_endpoints({ 's3' => { 'bucketCreationEndpoint' => 'https://pwops.example.com' },
                   's3Manager' => { 'external' => { 'endpoint' => 'https://s3-manager.example.com' } } },
                 'https://pwops.example.com', 'explicit endpoint')

# Without a bucket creation service the helper falls back to the S3 endpoint,
# which cannot create buckets; mono workers then get no endpoint at all.
expect_endpoints({ 's3Manager' => { 'enabled' => false } }, nil, 'no bucket creation service')

puts 'mono bucket creation tests passed'
