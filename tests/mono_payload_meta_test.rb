# Run: ruby tests/mono_payload_meta_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(values)
  Tempfile.create(['payload-meta', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '-f', f.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def mono(workers = {}, app = {})
  {'app' => app, 'workers' => {'type' => 'mono'}.merge(workers)}
end
# Every PAYLOAD_META_MAX_BYTES each Monocore worker Deployment receives, by name.
def mono_limits(documents)
  documents.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }.to_h do |d|
    env = d.dig('spec', 'template', 'spec', 'containers', 0, 'env') || []
    [d.dig('metadata', 'name'), env.select { |e| e['name'] == 'PAYLOAD_META_MAX_BYTES' }.map { |e| e['value'] }]
  end
end
# The value Core's application config carries, nil when it has none.
def core_limit(documents)
  configs = documents.select { |d| d['kind'] == 'ConfigMap' && d.dig('metadata', 'name') == 'patchworks-config' }
  assert(configs.size == 1, "Expected one Core application ConfigMap, got #{configs.map { |d| d.dig('metadata', 'name') }}")
  configs[0]['data']['PAYLOAD_META_MAX_BYTES']
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end

companies = {'companies' => [{'name' => 'shop'}]}

documents = render(mono(companies))
check({'patchworks-workers' => [], 'patchworks-workers-shop' => []}, mono_limits(documents),
  'without app.payloadMetaMaxBytes Monocore keeps its default')
check(nil, core_limit(documents), 'without app.payloadMetaMaxBytes Core keeps its default')

documents = render(mono(companies, 'payloadMetaMaxBytes' => '20480'))
check({'patchworks-workers' => ['20480'], 'patchworks-workers-shop' => ['20480']}, mono_limits(documents),
  'the hub and company workers enforce Core\'s limit')
check('20480', core_limit(documents), 'Core reads the same value')

check({'patchworks-workers' => ['20480']}, mono_limits(render(mono({}, 'payloadMetaMaxBytes' => 20480))),
  'a number renders as the same string')

check({'patchworks-workers' => ['30720'], 'patchworks-workers-shop' => ['30720']},
  mono_limits(render(mono(companies.merge('mono' => {'extraEnv' => [{'name' => 'PAYLOAD_META_MAX_BYTES', 'value' => '30720'}]}),
    'payloadMetaMaxBytes' => '20480'))),
  'workers.mono.extraEnv replaces the value for every worker')

check({'patchworks-workers' => ['20480'], 'patchworks-workers-shop' => ['20480'], 'patchworks-workers-tuned' => ['40960']},
  mono_limits(render(mono({'companies' => [
    {'name' => 'shop'},
    {'name' => 'tuned', 'extraEnv' => [{'name' => 'PAYLOAD_META_MAX_BYTES', 'value' => '40960'}]}
  ]}, 'payloadMetaMaxBytes' => '20480'))),
  'a company whose extraEnv sets the variable keeps that value alone')

puts 'Mono workers enforce Core\'s payload meta limit'
