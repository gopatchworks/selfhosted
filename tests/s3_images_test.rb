# Run: ruby tests/s3_images_test.rb (Helm required; no cluster access).
# MinIO no longer publishes minio/minio or minio/mc, so the in-cluster S3 server
# and its bucket setup Job must use pinned images that can still be pulled.
require 'json'
require 'yaml'
require 'open3'
require 'tempfile'

ROOT = File.expand_path('..', __dir__)

def assert(value, message)
  raise message unless value
end

# Container images keyed by "<kind>/<component>/<container>".
def images(values = {})
  Tempfile.create(['s3-images', '.yaml']) do |file|
    file.write(JSON.generate(values))
    file.flush
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', File.join(ROOT, 'charts/patchworks-infra'),
      '--namespace', 'install', '-f', file.path
    )
    raise error unless status.success?

    found = {}
    YAML.load_stream(output).compact.each do |document|
      spec = document.dig('spec', 'template', 'spec') || document.dig('spec', 'jobTemplate', 'spec', 'template', 'spec')
      next unless spec

      component = document.dig('metadata', 'labels', 'app.kubernetes.io/component')
      (spec.fetch('initContainers', []) + spec.fetch('containers', [])).each do |container|
        found["#{document['kind']}/#{component}/#{container['name']}"] = container
      end
    end
    found
  end
end

defaults = images
server = defaults['Deployment/s3/minio']
setup = defaults['Job/s3-setup/mc']
assert(server, 'S3 Deployment container is missing')
assert(setup, 's3-setup Job container is missing')
assert(server['image'] == 'pgsty/silo:RELEASE.2026-11-12T00-00-00Z', "unexpected S3 server image #{server['image']}")
assert(setup['image'] == 'pgsty/mc:RELEASE.2026-11-12T00-00-00Z', "unexpected S3 setup image #{setup['image']}")
assert(setup['imagePullPolicy'] == 'IfNotPresent', "unexpected S3 setup pull policy #{setup['imagePullPolicy']}")

stale = defaults.select { |_name, container| container['image'].match?(%r{\A(docker\.io/|quay\.io/)?minio/}) }
assert(stale.empty?, "images MinIO no longer publishes: #{stale.map { |name, c| "#{name}=#{c['image']}" }.join(', ')}")
[server, setup].each do |container|
  assert(!container['image'].end_with?(':latest'), "#{container['image']} is not pinned")
end

overridden = images('s3' => {
  'image' => {'repository' => 'registry.example.com/silo', 'tag' => 'fixture', 'pullPolicy' => 'Always'},
  'setup' => {'image' => {'repository' => 'registry.example.com/mc', 'tag' => 'fixture', 'pullPolicy' => 'Always'}}
})
assert(overridden['Deployment/s3/minio']['image'] == 'registry.example.com/silo:fixture', 's3.image override ignored')
assert(overridden['Job/s3-setup/mc']['image'] == 'registry.example.com/mc:fixture', 's3.setup.image override ignored')
assert(overridden['Job/s3-setup/mc']['imagePullPolicy'] == 'Always', 's3.setup.image.pullPolicy override ignored')

# One values file serves both charts, so the app chart's passthrough must match.
infra = YAML.load_file(File.join(ROOT, 'charts/patchworks-infra/values.yaml'))['s3']
app = YAML.load_file(File.join(ROOT, 'charts/patchworks-app/values.yaml'))['s3']
%w[image setup].each do |key|
  assert(infra[key] == app[key], "s3.#{key} differs between charts: infra=#{infra[key].inspect} app=#{app[key].inspect}")
end

puts 'S3 image render tests passed'
