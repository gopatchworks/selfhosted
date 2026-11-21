# Run: ruby tests/mono_all_companies_test.rb (Helm required; no cluster access).
#
# workers.mono.allCompanies: Core turns Monocore (and its scheduler) on for every
# company it provisions, the Core seed job does the same for companies with no
# stored value, the hub admits every trigger, and the Core processors Monocore
# companies don't need are left out. Off, or outside workers.type=mono, nothing
# renders differently.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
FLAGS = %w[MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES MONOCORE_SCHEDULER_AUTO_ENABLE_FOR_NEW_COMPANIES].freeze
def assert(value, message)
  raise message unless value
end
def render_raw(values)
  Tempfile.create(['all-companies', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS, '-f', f.path)
  end
end
def render(values)
  output, error, status = render_raw(values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def mono(all = nil, type: 'mono', **mono_extra)
  mono_values = mono_extra.transform_keys(&:to_s)
  mono_values['allCompanies'] = all unless all.nil?
  {'workers' => {'type' => type, 'mono' => mono_values}, 'seeds' => {'core' => {'enabled' => true}}}
end
def find(docs, kind, name)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name }
end
def names(docs, kind)
  docs.select { |d| d['kind'] == kind }.map { |d| d.dig('metadata', 'name') }.sort
end
def container(doc)
  spec = doc.dig('spec', 'template', 'spec') || doc.dig('spec', 'jobTemplate', 'spec', 'template', 'spec')
  spec['containers'].first
end
def env(doc)
  container(doc).fetch('env', []).to_h { |e| [e['name'], e['value']] }
end
def config(docs)
  find(docs, 'ConfigMap', 'patchworks-config').fetch('data')
end
def topology_queues(docs)
  cm = docs.find { |d| d['kind'] == 'ConfigMap' && d.dig('metadata', 'name').end_with?('rabbitmq-topology') }
  YAML.safe_load(cm['data'].values.first).fetch('queues').map { |q| q['name'] }
end

# Off: identical to a mono render without the key, and no flag settings anywhere.
off = render(mono)
check(off, render(mono({'enabled' => false})), 'allCompanies.enabled=false render')
FLAGS.each { |flag| assert(!config(off).key?(flag), "#{flag} set while off") }
check(nil, env(find(off, 'Job', 'patchworks-core-seeds'))['MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES'], 'seed job flag while off')
assert(!container(find(off, 'Job', 'patchworks-core-seeds'))['command'].last.include?('monocore:auto-enable'), 'seed job runs auto-enable while off')
assert(names(off, 'Deployment').include?('patchworks-processor-short-processor'), 'short-processor missing while off')
assert(names(off, 'CronJob').include?('patchworks-gateway-scheduler'), 'gateway schedule missing while off')
assert(!env(find(off, 'Deployment', 'patchworks-workers')).key?('SCHEDULER_ADMIT_ALL_TRIGGERS'), 'admit-all set while off')

# Only mono: another worker type ignores the key.
%w[standalone microservice].each do |type|
  check(render(mono(type: type)), render(mono({'enabled' => true}, type: type)), "#{type} render with allCompanies on")
end

# On: flags for Core pods and hook jobs, seed step, admit-all, processors left out.
on = render(mono({'enabled' => true}))
check('true', config(on)['MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES'], 'ConfigMap auto-enable')
check('true', config(on)['MONOCORE_SCHEDULER_AUTO_ENABLE_FOR_NEW_COMPANIES'], 'ConfigMap scheduler auto-enable')
%w[patchworks-core-seeds patchworks-core-migrations].each do |job|
  check('true', env(find(on, 'Job', job))['MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES'], "#{job} auto-enable")
  check('true', env(find(on, 'Job', job))['MONOCORE_SCHEDULER_AUTO_ENABLE_FOR_NEW_COMPANIES'], "#{job} scheduler auto-enable")
end
seed = container(find(on, 'Job', 'patchworks-core-seeds'))['command'].last
assert(seed.start_with?('php artisan db:seed --force && php artisan migrate:tenants --create'), "seed command lost its prefix: #{seed}")
assert(seed.include?("migrate:tenants --create --no-interaction && if php artisan list --raw | grep -q '^monocore:auto-enable '; then php artisan monocore:auto-enable;"),
       "seed command does not run auto-enable after migrate:tenants --create: #{seed}")
check('true', env(find(on, 'Deployment', 'patchworks-workers'))['SCHEDULER_ADMIT_ALL_TRIGGERS'], 'hub admit-all')
check(names(off, 'Deployment') - ['patchworks-processor-short-processor'], names(on, 'Deployment'), 'Deployments')
check(names(off, 'CronJob') - %w[patchworks-short-processor-scheduler patchworks-gateway-scheduler], names(on, 'CronJob'), 'CronJobs')
assert(names(on, 'Deployment').include?('patchworks-processor-gateway'), 'gateway processor left out with its schedule')
assert(!find(on, 'ConfigMap', 'patchworks-processor-short-processor-supervisord'), 'short-processor supervisord ConfigMap still renders')
check(topology_queues(off) - ['short-processor'], topology_queues(on), 'RabbitMQ topology queues')

# The hub's config checksum ignores the key, so turning it on rolls the hub for
# its env alone, and adding the key's defaults rolls nothing.
hub_checksum = ->(docs) { find(docs, 'Deployment', 'patchworks-workers').dig('spec', 'template', 'metadata', 'annotations', 'checksum/config') }
check(hub_checksum.call(off), hub_checksum.call(on), 'hub config checksum')

# Without the scheduler: monocore alone, no admit-all.
no_sched = render(mono({'enabled' => true, 'scheduler' => false}))
check('true', config(no_sched)['MONOCORE_AUTO_ENABLE_FOR_NEW_COMPANIES'], 'auto-enable without scheduler')
check('false', config(no_sched)['MONOCORE_SCHEDULER_AUTO_ENABLE_FOR_NEW_COMPANIES'], 'scheduler auto-enable without scheduler')
assert(!env(find(no_sched, 'Deployment', 'patchworks-workers')).key?('SCHEDULER_ADMIT_ALL_TRIGGERS'), 'admit-all without scheduler')

# An explicit admitAllTriggers still applies on its own.
explicit = render(mono(scheduler: {'admitAllTriggers' => true}))
check('true', env(find(explicit, 'Deployment', 'patchworks-workers'))['SCHEDULER_ADMIT_ALL_TRIGGERS'], 'explicit admit-all')

# The scheduler feature needs the hub's scheduler.
_, error, status = render_raw(mono({'enabled' => true}, scheduler: {'mode' => 'disabled'}))
assert(!status.success? && error.include?('workers.mono.allCompanies.scheduler needs the hub'), "expected scheduler failure: #{error}")
render(mono({'enabled' => true, 'scheduler' => false}, scheduler: {'mode' => 'disabled'}))

# The lists are overridable: empty lists leave every processor in place.
kept = render(mono({'enabled' => true, 'disableProcessors' => [], 'disableSchedulers' => []}))
check(names(off, 'Deployment'), names(kept, 'Deployment'), 'Deployments with empty lists')
check(names(off, 'CronJob'), names(kept, 'CronJob'), 'CronJobs with empty lists')

puts 'mono_all_companies_test.rb: ok'
