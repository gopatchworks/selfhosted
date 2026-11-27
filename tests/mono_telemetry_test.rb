# Run: ruby tests/mono_telemetry_test.rb (Helm required; no cluster access).
#
# telemetry.enabled: Monocore records the runs it executes into Core's
# telemetry streams, so every Monocore worker, the hub and each company, gets
# TELEMETRY_ENABLED and TELEMETRY_CLUSTER, the two Core's ConfigMap sets, and
# never a ClickHouse setting or password. A worker's extraEnv entry takes the
# place of the chart's.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
WORKERS = %w[patchworks-workers patchworks-workers-shop].freeze
# The fixture generates no credentials, so the ClickHouse logins come from a Secret.
CLICKHOUSE = {'clickhouse' => {'enabled' => true, 'auth' => {'existingSecret' => {'name' => 'fixture-clickhouse'}}}}.freeze
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render(values)
  Tempfile.create(['mono-telemetry', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS, '-f', f.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def values(telemetry: nil, mono: {}, company: {})
  base = {'workers' => {'type' => 'mono', 'companies' => [{'name' => 'shop'}.merge(company)], 'mono' => mono}}
  return base unless telemetry
  base.merge(CLICKHOUSE, 'telemetry' => {'enabled' => true}.merge(telemetry))
end
# Every TELEMETRY_* each Monocore worker sets, by Deployment name, each name
# with all its values, so a duplicated name shows up as two values.
def telemetry_env(docs)
  docs.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }.to_h do |d|
    env = d.dig('spec', 'template', 'spec', 'containers', 0, 'env')
    mine = env.select { |e| e['name'].start_with?('TELEMETRY_') }
    [d.dig('metadata', 'name'), mine.group_by { |e| e['name'] }.transform_values { |es| es.map { |e| e['value'] } }]
  end
end

off = telemetry_env(render(values))
check(WORKERS, off.keys.sort, 'Monocore workers rendered')
off.each { |name, env| check({}, env, "#{name} telemetry env while telemetry is off") }

on = telemetry_env(render(values(telemetry: {'cluster' => 'hfox'})))
check(WORKERS, on.keys.sort, 'Monocore workers rendered with telemetry')
on.each do |name, env|
  check({'TELEMETRY_ENABLED' => ['true'], 'TELEMETRY_CLUSTER' => ['hfox']}, env, "#{name} telemetry env")
end

# The default cluster name, and one YAML reads as a number, render as strings.
telemetry_env(render(values(telemetry: {}))).each do |name, env|
  check(['selfhosted'], env['TELEMETRY_CLUSTER'], "#{name} default TELEMETRY_CLUSTER")
end
telemetry_env(render(values(telemetry: {'cluster' => 12345}))).each do |name, env|
  check(['12345'], env['TELEMETRY_CLUSTER'], "#{name} numeric TELEMETRY_CLUSTER")
end

# The operator's own entries win, once each: workers.mono.extraEnv on every
# worker, a company's extraEnv on its worker alone.
own = telemetry_env(render(values(
  telemetry: {'cluster' => 'hfox'},
  mono: {'extraEnv' => [{'name' => 'TELEMETRY_ENABLED', 'value' => 'false'}]},
  company: {'extraEnv' => [{'name' => 'TELEMETRY_CLUSTER', 'value' => 'shop-only'}]}
)))
check({'TELEMETRY_ENABLED' => ['false'], 'TELEMETRY_CLUSTER' => ['hfox']}, own['patchworks-workers'], 'hub with workers.mono.extraEnv')
check({'TELEMETRY_ENABLED' => ['false'], 'TELEMETRY_CLUSTER' => ['shop-only']}, own['patchworks-workers-shop'], 'company with its own extraEnv')

puts 'ok'
