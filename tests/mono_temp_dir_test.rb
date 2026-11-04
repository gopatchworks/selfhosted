# Run: ruby tests/mono_temp_dir_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(values, failure: nil)
  Tempfile.create(['tempdir', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '-f', f.path)
    if failure
      assert(!status.success? && error.include?(failure), "Expected #{failure}: #{error}")
      return
    end
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def mono(workers = {})
  {'workers' => {'type' => 'mono'}.merge(workers)}
end
# Each Monocore worker Deployment's /tmp size limit, /tmp mount and
# WORKER_TEMP_DIR_LIMIT_BYTES, by name.
def temp_dirs(values)
  render(values).select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }.to_h do |d|
    spec = d.dig('spec', 'template', 'spec')
    container = spec['containers'][0]
    tmp = (spec['volumes'].find { |v| v['name'] == 'tmp' } || {})['emptyDir']
    mount = container['volumeMounts'].find { |m| m['mountPath'] == '/tmp' }
    limits = container['env'].select { |e| e['name'] == 'WORKER_TEMP_DIR_LIMIT_BYTES' }.map { |e| e['value'] }
    [d.dig('metadata', 'name'), [tmp && tmp['sizeLimit'], mount && mount['name'], limits]]
  end
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end

check({'patchworks-workers' => ['10Gi', 'tmp', ['10737418240']]}, temp_dirs(mono),
  '/tmp is a 10Gi emptyDir and the worker reads its size in bytes')

dirs = temp_dirs(mono(
  'mono' => {'tmp' => {'sizeLimit' => '20Gi'}},
  'companies' => [
    {'name' => 'shop'},
    {'name' => 'large', 'tmp' => {'sizeLimit' => '40Gi'}},
    {'name' => 'bytes', 'tmp' => {'sizeLimit' => 5368709120}},
    {'name' => 'tuned', 'extraEnv' => [{'name' => 'WORKER_TEMP_DIR_LIMIT_BYTES', 'value' => '0'}]}
  ]
))
check({
  'patchworks-workers' => ['20Gi', 'tmp', ['21474836480']],
  'patchworks-workers-shop' => ['20Gi', 'tmp', ['21474836480']],
  'patchworks-workers-large' => ['40Gi', 'tmp', ['42949672960']],
  'patchworks-workers-bytes' => ['5368709120', 'tmp', ['5368709120']],
  'patchworks-workers-tuned' => ['20Gi', 'tmp', ['0']]
}, dirs, 'each worker follows its own tmp.sizeLimit unless its extraEnv sets the variable')

{'512Mi' => '536870912', '1.5Gi' => '1610612736', '20G' => '20000000000', '10737418240' => '10737418240'}.each do |limit, expected|
  check([expected], temp_dirs(mono('mono' => {'tmp' => {'sizeLimit' => limit}}))['patchworks-workers'][2], "WORKER_TEMP_DIR_LIMIT_BYTES for a #{limit.inspect} /tmp")
end
check(['20Pi', 'tmp', ['0']], temp_dirs(mono('mono' => {'tmp' => {'sizeLimit' => '20Pi'}, 'extraEnv' => [{'name' => 'WORKER_TEMP_DIR_LIMIT_BYTES', 'value' => '0'}]}))['patchworks-workers'],
  'a size the chart cannot read renders with the variable in extraEnv')
check(['10Gi', 'tmp', ['0']], temp_dirs(mono('extraEnv' => [{'name' => 'WORKER_TEMP_DIR_LIMIT_BYTES', 'value' => '0'}]))['patchworks-workers'],
  'workers.extraEnv replaces the derived variable')

render(mono('mono' => {'tmp' => {'sizeLimit' => '20Pi'}}), failure: 'cannot derive WORKER_TEMP_DIR_LIMIT_BYTES for workers')
render(mono('mono' => {'tmp' => {'sizeLimit' => ''}}), failure: 'workers tmp.sizeLimit is required')
render(mono('companies' => [{'name' => 'shop', 'tmp' => {'sizeLimit' => '1Pi'}}]), failure: 'cannot derive WORKER_TEMP_DIR_LIMIT_BYTES for workers-shop')
puts 'Mono workers bound /tmp and tell Monocore its size'
