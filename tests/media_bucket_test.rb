# Run: ruby tests/media_bucket_test.rb (Helm only; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)
MAIN = 'patchworks-payloads'
MEDIA = 'patchworks-media'

def assert(value, message)
  raise message unless value
end

def render(values = {})
  base = {
    's3' => { 'enabled' => false, 'external' => { 'bucket' => MAIN, 'endpoint' => 'https://s3.example.com' } },
    'workers' => { 'type' => 'mono' },
  }
  merged = base.merge(values) { |_key, left, right| left.is_a?(Hash) && right.is_a?(Hash) ? left.merge(right) : right }
  Tempfile.create(['media-bucket', '.yaml']) do |file|
    file.write(JSON.generate(merged))
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

# Every value a variable has, from ConfigMap data and container env, keyed by where it is.
def values_of(documents, variable)
  found = {}
  documents.each do |document|
    name = "#{document['kind']}/#{document.dig('metadata', 'name')}"
    found[name] = document.dig('data', variable) if document['kind'] == 'ConfigMap' && document.dig('data', variable)
    pod = document.dig('spec', 'template', 'spec') || document.dig('spec', 'jobTemplate', 'spec', 'template', 'spec')
    next unless pod
    (pod['containers'].to_a + pod['initContainers'].to_a).each do |container|
      container['env'].to_a.each do |entry|
        found["#{name}/#{container['name']}"] = entry['value'] if entry['name'] == variable && entry.key?('value')
      end
    end
  end
  found
end

def expect_all(documents, variable, expected, description)
  found = values_of(documents, variable)
  assert(!found.empty?, "#{description}: nothing renders #{variable}")
  wrong = found.reject { |_where, value| value == expected }
  assert(wrong.empty?, "#{description}: #{variable} should be #{expected.inspect} but is #{wrong.inspect}")
end

# Unset, AWS_BUCKET stays the main bucket, as before.
default = render
expect_all(default, 'AWS_BUCKET', MAIN, 'default')

# Set, AWS_BUCKET moves to the media bucket in every ConfigMap and Job env that has it,
# and nothing else that reads the main bucket moves with it.
media = render('s3' => { 'mediaBucket' => MEDIA })
expect_all(media, 'AWS_BUCKET', MEDIA, 'media bucket')
expect_all(media, 'PAYLOADS_AWS_BUCKET', MAIN, 'media bucket leaves payloads')
storegen = media.map(&:to_yaml).join
assert(storegen.include?("s3_bucket=\"#{MAIN}\""), "Monocore's store generator should keep the main bucket")
assert(!storegen.include?("s3_bucket=\"#{MEDIA}\""), "Monocore's store generator must not take the media bucket")

puts 'media bucket tests passed'
