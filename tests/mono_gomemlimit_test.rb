# Run: ruby tests/mono_gomemlimit_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(values, failure: nil)
  Tempfile.create(['gomemlimit', '.yaml']) do |f|
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
# GOMEMLIMIT of each Monocore worker Deployment, by name.
def go_mem_limits(values)
  render(values).select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }.to_h do |d|
    env = d.dig('spec', 'template', 'spec', 'containers', 0, 'env')
    [d.dig('metadata', 'name'), (env.find { |e| e['name'] == 'GOMEMLIMIT' } || {})['value']]
  end
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end

check({'patchworks-workers' => nil}, go_mem_limits(mono), 'GOMEMLIMIT is unset without a memory limit')

limits = go_mem_limits(mono(
  'resources' => {'requests' => {'memory' => '900Mi'}, 'limits' => {'memory' => '2Gi'}},
  'companies' => [
    {'name' => 'shop'},
    {'name' => 'small', 'resources' => {'limits' => {'memory' => '1Gi'}}},
    {'name' => 'tuned', 'goMemLimit' => '1500MiB'},
    {'name' => 'unbounded', 'goMemLimit' => false}
  ]
))
check({
  'patchworks-workers' => '1843MiB',
  'patchworks-workers-shop' => '1843MiB',
  'patchworks-workers-small' => '921MiB',
  'patchworks-workers-tuned' => '1500MiB',
  'patchworks-workers-unbounded' => 'off'
}, limits, 'GOMEMLIMIT is 90% of each worker memory limit unless a company sets its own')

{'512Mi' => '460MiB', '1.5Gi' => '1382MiB', '2G' => '1716MiB', 2147483648 => '1843MiB', '2147483648' => '1843MiB'}.each do |limit, expected|
  check({'patchworks-workers' => expected}, go_mem_limits(mono('resources' => {'limits' => {'memory' => limit}})), "GOMEMLIMIT for a #{limit.inspect} memory limit")
end
check({'patchworks-workers' => '1700MiB'}, go_mem_limits(mono('resources' => {'limits' => {'memory' => '2Gi'}}, 'mono' => {'goMemLimit' => '1700MiB'})), 'workers.mono.goMemLimit overrides the derived value')

render(mono('mono' => {'goMemLimit' => '1700Mi'}), failure: 'is not a GOMEMLIMIT the Go runtime accepts')
render(mono('mono' => {'goMemLimit' => true}), failure: 'is not a GOMEMLIMIT the Go runtime accepts')
render(mono('resources' => {'limits' => {'memory' => '2Ei'}}), failure: 'cannot derive GOMEMLIMIT for workers')
render(mono('resources' => {'limits' => {'memory' => '100'}}), failure: 'too small to derive GOMEMLIMIT')
render(mono('companies' => [{'name' => 'shop', 'resources' => {'limits' => {'memory' => '1Ei'}}}]), failure: 'cannot derive GOMEMLIMIT for workers-shop')
puts 'Mono workers derive GOMEMLIMIT from their memory limits'
