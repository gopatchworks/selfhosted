# Run: ruby tests/mono_optional_shapes_test.rb (Helm required; no cluster access).
#
# workers.mono.enable: Monocore's optional shapes, which it registers no step
# runner for unless told to, as WORKER_ENABLE_* on the hub and every company.
# A shape left null follows workers.mono.allCompanies; catchAtRunEnd stays off
# with it. An explicit value wins either way, and a worker's extraEnv entry
# takes the place of the chart's.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
SHAPES = {
  'merge' => 'WORKER_ENABLE_MERGE',
  'dataTransform' => 'WORKER_ENABLE_DATA_TRANSFORM',
  'crossReference' => 'WORKER_ENABLE_CROSS_REFERENCE',
  'syncSubprocess' => 'WORKER_ENABLE_SYNC_SUBPROCESS'
}.freeze
CATCH = 'WORKER_ENABLE_CATCH_AT_RUN_END'
FLAGS = (SHAPES.values + [CATCH]).freeze
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render_raw(values)
  Tempfile.create(['optional-shapes', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS, '-f', f.path)
  end
end
def render(values)
  output, error, status = render_raw(values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def mono(mono_values = {}, workers = {})
  {'workers' => {'type' => 'mono', 'companies' => [{'name' => 'shop'}], 'mono' => mono_values}.merge(workers)}
end
def worker_deployments(docs)
  docs.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }
end
# Every value each Monocore worker sets for each flag, by Deployment name, so a
# duplicated name shows up as two values.
def flags(docs)
  worker_deployments(docs).to_h do |d|
    env = d.dig('spec', 'template', 'spec', 'containers', 0, 'env')
    [d.dig('metadata', 'name'), FLAGS.to_h { |flag| [flag, env.select { |e| e['name'] == flag }.map { |e| e['value'] }] }]
  end
end
def expected(shapes, catch_at_run_end = 'false', overrides = {})
  FLAGS.to_h { |flag| [flag, [flag == CATCH ? catch_at_run_end : shapes]] }.merge(overrides)
end
def checksum(docs, name)
  worker_deployments(docs).find { |d| d.dig('metadata', 'name') == name }.dig('spec', 'template', 'metadata', 'annotations', 'checksum/config')
end
WORKERS = %w[patchworks-workers patchworks-workers-shop].freeze
ALL = {'allCompanies' => {'enabled' => true}}.freeze

# Off by default: Monocore's own defaults, spelled out on the hub and company.
off = render(mono)
check(WORKERS.to_h { |w| [w, expected('false')] }, flags(off), 'defaults without allCompanies')

# allCompanies turns the five shapes on, but not catchAtRunEnd.
on = render(mono(ALL))
check(WORKERS.to_h { |w| [w, expected('true')] }, flags(on), 'defaults with allCompanies')

# An explicit value wins over allCompanies either way; null and absent follow it.
explicit = render(mono(ALL.merge('enable' => {'merge' => false, 'dataTransform' => 'false', 'crossReference' => nil, 'catchAtRunEnd' => true})))
check(WORKERS.to_h { |w| [w, expected('true', 'true', {'WORKER_ENABLE_MERGE' => ['false'], 'WORKER_ENABLE_DATA_TRANSFORM' => ['false']})] },
      flags(explicit), 'explicit false and true with allCompanies, strings accepted')
explicit_off = render(mono('enable' => {'merge' => true, 'syncSubprocess' => 'true'}))
check(WORKERS.to_h { |w| [w, expected('false', 'false', {'WORKER_ENABLE_MERGE' => ['true'], 'WORKER_ENABLE_SYNC_SUBPROCESS' => ['true']})] },
      flags(explicit_off), 'explicit true without allCompanies, strings accepted')
check(WORKERS.to_h { |w| [w, expected('true')] }, flags(render(mono('enable' => SHAPES.keys.to_h { |k| [k, true] }))),
      'every shape on without allCompanies')

# Anything else fails.
_, error, status = render_raw(mono('enable' => {'merge' => 'yes'}))
assert(!status.success? && error.include?('workers.mono.enable.merge must be true, false or null, got yes'), "expected enable failure: #{error}")

# A worker's extraEnv entry replaces the chart's, leaving one entry: workers'
# and mono's for every worker, a company's for that company alone.
overridden = render(mono(ALL.merge('extraEnv' => [{'name' => 'WORKER_ENABLE_SYNC_SUBPROCESS', 'value' => 'false'}]),
                         'extraEnv' => [{'name' => 'WORKER_ENABLE_MERGE', 'value' => 'false'}],
                         'companies' => [{'name' => 'shop', 'extraEnv' => [{'name' => 'WORKER_ENABLE_CROSS_REFERENCE', 'value' => 'false'},
                                                                          {'name' => CATCH, 'value' => 'true'}]}]))
check({
  'patchworks-workers' => expected('true', 'false', {'WORKER_ENABLE_MERGE' => ['false'], 'WORKER_ENABLE_SYNC_SUBPROCESS' => ['false']}),
  'patchworks-workers-shop' => expected('true', 'true', {'WORKER_ENABLE_MERGE' => ['false'], 'WORKER_ENABLE_SYNC_SUBPROCESS' => ['false'],
                                                         'WORKER_ENABLE_CROSS_REFERENCE' => ['false']})
}, flags(overridden), 'extraEnv overrides')

# A company-only release (no hub) gets them too.
check({'patchworks-workers-shop' => expected('true')},
      flags(render(mono(ALL.merge('scheduler' => {'mode' => 'disabled'}, 'allCompanies' => {'enabled' => true, 'scheduler' => false}),
                        'hub' => {'enabled' => false}))),
      'company-only release')

# Data Processing is left out, since Core and the Dashboard are dropping it:
# the chart renders no flag for it, even when asked, and a worker's extraEnv
# entry still reaches it once.
DATA_PROCESSING = 'WORKER_ENABLE_DATA_PROCESSING'
def env_values(docs, name)
  worker_deployments(docs).to_h do |d|
    [d.dig('metadata', 'name'), d.dig('spec', 'template', 'spec', 'containers', 0, 'env').select { |e| e['name'] == name }.map { |e| e['value'] }]
  end
end
check(WORKERS.to_h { |w| [w, []] }, env_values(render(mono(ALL.merge('enable' => {'dataProcessing' => true}))), DATA_PROCESSING),
      'no Data Processing flag')
check(WORKERS.to_h { |w| [w, ['true']] },
      env_values(render(mono(ALL.merge('extraEnv' => [{'name' => DATA_PROCESSING, 'value' => 'true'}]))), DATA_PROCESSING),
      'Data Processing through extraEnv')

# The setting changes env alone, so it leaves the config checksum as it was.
WORKERS.each { |w| check(checksum(off, w), checksum(explicit_off, w), "#{w} config checksum") }

# Only Monocore workers read them.
%w[standalone microservice].each do |type|
  docs = render({'workers' => {'type' => type, 'mono' => ALL.merge('enable' => {'merge' => true})}})
  rendered = docs.flat_map do |d|
    spec = d.dig('spec', 'template', 'spec') || d.dig('spec', 'jobTemplate', 'spec', 'template', 'spec') || {}
    (spec['containers'] || []).flat_map { |c| (c['env'] || []).map { |e| e['name'] } }
  end
  check([], rendered & FLAGS, "#{type} WORKER_ENABLE_* env")
end

puts 'mono_optional_shapes_test.rb: ok'
