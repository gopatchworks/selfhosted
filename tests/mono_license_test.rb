# Run: ruby tests/mono_license_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
DEFAULT_SERVER = 'https://license.wearepatchworks.com'
def assert(value, message)
  raise message unless value
end
def render(values)
  Tempfile.create(['mono-license', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '-f', f.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def mono(license, workers = {})
  {'app' => {'license' => license}, 'workers' => {'type' => 'mono', 'companies' => [{'name' => 'shop'}]}.merge(workers)}
end
def mono_containers(documents)
  documents.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }.to_h do |d|
    [d.dig('metadata', 'name'), d.dig('spec', 'template', 'spec', 'containers', 0)]
  end
end
# Every LICENSE_* variable each Monocore worker Deployment receives, by name.
def mono_license_env(documents)
  mono_containers(documents).transform_values do |c|
    (c['env'] || []).select { |e| e['name'].start_with?('LICENSE_') }
  end
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}:\nexpected #{expected.inspect}\n     got #{actual.inspect}")
end
def both(env)
  {'patchworks-workers' => env, 'patchworks-workers-shop' => env}
end
def secret_ref(name, key)
  {'valueFrom' => {'secretKeyRef' => {'name' => name, 'key' => key}}}
end

check(both([
  {'name' => 'LICENSE_KEY', 'value' => 'inline-key'},
  {'name' => 'LICENSE_SERVER_URL', 'value' => DEFAULT_SERVER},
]), mono_license_env(render(mono('key' => 'inline-key'))),
  'an inline key reaches the hub and company workers, with the default server URL')

check(both([
  {'name' => 'LICENSE_KEY'}.merge(secret_ref('fixture-license', 'LICENSE_KEY')),
  {'name' => 'LICENSE_SERVER_URL', 'value' => DEFAULT_SERVER},
]), mono_license_env(render(mono('key' => 'ignored', 'existingSecret' => {'name' => 'fixture-license'}))),
  'an existing Secret supplies the key, and the server URL stays inline without serverUrlKey')

check(both([
  {'name' => 'LICENSE_KEY'}.merge(secret_ref('fixture-license', 'licence-key')),
  {'name' => 'LICENSE_SERVER_URL'}.merge(secret_ref('fixture-license', 'licence-server')),
]), mono_license_env(render(mono('existingSecret' => {'name' => 'fixture-license', 'keyKey' => 'licence-key', 'serverUrlKey' => 'licence-server'}))),
  'an existing Secret with serverUrlKey supplies both values under their keys')

check(both([{'name' => 'LICENSE_SERVER_URL', 'value' => 'https://licence.example.test'}]),
  mono_license_env(render(mono('serverUrl' => 'https://licence.example.test'))),
  'without a key the workers get only the server URL')

check({'patchworks-workers-shop' => [
  {'name' => 'LICENSE_KEY', 'value' => 'inline-key'},
  {'name' => 'LICENSE_SERVER_URL', 'value' => DEFAULT_SERVER},
]}, mono_license_env(render(mono({'key' => 'inline-key'}, 'hub' => {'enabled' => false}))),
  'a company-only release is licensed too')

check(both([
  {'name' => 'LICENSE_KEY', 'value' => 'inline-key'},
  {'name' => 'LICENSE_SERVER_URL', 'value' => DEFAULT_SERVER},
]), mono_license_env(render(mono('key' => 'inline-key', 'active' => 'false'))),
  'app.license.active does not decide whether Monocore gets its licence')

documents = render(mono('key' => 'inline-key'))
mono_containers(documents).each do |name, c|
  check('/readyz', c.dig('readinessProbe', 'httpGet', 'path'), "#{name} readiness waits for the licence server")
  check('/healthz', c.dig('livenessProbe', 'httpGet', 'path'), "#{name} liveness never depends on the licence")
end
core = documents.find { |d| d['kind'] == 'ConfigMap' && d.dig('metadata', 'name') == 'patchworks-config' }
check([DEFAULT_SERVER, DEFAULT_SERVER, 'true'], core['data'].values_at('LICENSE_SERVER_URL', 'LICENSE_URL', 'LICENSE_ACTIVE'),
  'Core and Fabric keep their licence settings')

puts 'Monocore workers are licensed and wait for the licence server before taking requests'
