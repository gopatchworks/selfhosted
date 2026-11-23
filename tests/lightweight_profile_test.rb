# Run: ruby tests/lightweight_profile_test.rb (Helm required; no cluster access).
#
# profile=lightweight: the gateway Deployment also serves the start routes
# (APP_DOMAINS=start) and the start Service selects its pods; <fullname>-processor
# consumes every enabled processor queue in two pools and runs each processor's
# schedule in place of the processor Deployments and schedule CronJobs.
# profile=standard renders as before.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render_raw(values)
  Tempfile.create(['lightweight', '.yaml']) do |f|
    f.write(values.empty? ? "" : YAML.dump(values)); f.flush
    Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS, '-f', f.path)
  end
end
def render(values)
  output, error, status = render_raw(values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def fails(values, message)
  _, error, status = render_raw(values)
  assert(!status.success? && error.include?(message), "expected failure #{message.inspect}: #{error}")
end
def find(docs, kind, name)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name }
end
def names(docs, kind)
  docs.select { |d| d['kind'] == kind }.map { |d| d.dig('metadata', 'name') }.sort
end
def env(doc)
  doc.dig('spec', 'template', 'spec', 'containers').first.fetch('env', []).to_h { |e| [e['name'], e['value']] }
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
# The supervisord programs of the lightweight worker, as {name => {key => value}}.
def programs(docs)
  conf = find(docs, 'ConfigMap', 'patchworks-processor-supervisord').dig('data', 'supervisord.conf')
  conf.split(/^\[program:/).drop(1).to_h do |block|
    name, body = block.split("]\n", 2)
    [name, body.lines.map(&:strip).reject(&:empty?).to_h { |l| l.split('=', 2) }]
  end
end
LIGHT = {'profile' => 'lightweight'}.freeze

# Standard: unchanged, with the profile named explicitly or defaulted.
standard = render({})
check(standard, render({'profile' => 'standard'}), 'explicit standard profile')
assert(!find(standard, 'Deployment', 'patchworks-processor'), 'lightweight worker renders in standard')
assert(!env(find(standard, 'Deployment', 'patchworks-gateway')).key?('APP_DOMAINS'), 'APP_DOMAINS set in standard')
fails({'profile' => 'tiny'}, 'profile must be standard or lightweight')

# Lightweight: one web and one worker Deployment for Core, no CronJobs.
light = render(LIGHT)
core_deployments = names(light, 'Deployment').reject { |n| %w[patchworks-fabric patchworks-dashboard patchworks-s3-manager patchworks-workers].include?(n) }
check(%w[patchworks-gateway patchworks-processor], core_deployments, 'Core Deployments')
check([], names(light, 'CronJob'), 'CronJobs')
check(names(standard, 'Service'), names(light, 'Service'), 'Services')
assert(names(light, 'ConfigMap').none? { |n| n.start_with?('patchworks-processor-') && n != 'patchworks-processor-supervisord' }, 'processor ConfigMaps remain')

gateway = find(light, 'Deployment', 'patchworks-gateway')
check('gateway', env(gateway)['APP_DOMAIN'], 'web APP_DOMAIN')
check('start', env(gateway)['APP_DOMAINS'], 'web APP_DOMAINS')
check(gateway.dig('spec', 'selector', 'matchLabels'), find(light, 'Service', 'patchworks-start').dig('spec', 'selector'), 'start Service selector')

worker = find(light, 'Deployment', 'patchworks-processor')
assert(!env(worker).key?('APP_DOMAIN') && !env(worker).key?('APP_NAME'), 'worker pod sets APP_DOMAIN or APP_NAME for every program')
progs = programs(light)
check(%w[queue queue-slow schedule-gateway schedule-long-processor schedule-medium-processor schedule-scheduler schedule-short-processor schedule-start], progs.keys.sort, 'supervisord programs')
assert(progs['queue']['command'].include?('--queue=scheduler,start,gateway,short-processor,logging '), "main pool queues: #{progs['queue']['command']}")
check('APP_DOMAIN="gateway",RABBITMQ_QUEUE="gateway"', progs['queue']['environment'], 'main pool domain')
check('2', progs['queue']['numprocs'], 'main pool processes')
assert(progs['queue-slow']['command'].include?('--queue=medium-processor,long-processor '), "slow pool queues: #{progs['queue-slow']['command']}")
check('APP_DOMAIN="medium-processor",RABBITMQ_QUEUE="medium-processor"', progs['queue-slow']['environment'], 'slow pool domain')
check('1', progs['queue-slow']['numprocs'], 'slow pool processes')
check('/bin/sh /etc/patchworks/schedule.sh 300', progs['schedule-scheduler']['command'], 'schedule command')
check('APP_DOMAIN="scheduler",RABBITMQ_QUEUE="scheduler"', progs['schedule-scheduler']['environment'], 'schedule domain')
script = find(light, 'ConfigMap', 'patchworks-processor-supervisord').dig('data', 'schedule.sh')
assert(script.include?('timeout "$deadline" /usr/local/bin/php /var/www/html/artisan schedule:run'), "schedule script: #{script}")

# The pod rolls when the programs change, not only the app config.
checksum = ->(docs) { find(docs, 'Deployment', 'patchworks-processor').dig('spec', 'template', 'metadata', 'annotations', 'checksum/supervisord') }
assert(checksum.call(light) != checksum.call(render(deep_merge(LIGHT, 'lightweight' => {'worker' => {'processes' => 3}}))), 'checksum ignores processes')
assert(checksum.call(light) != checksum.call(render(deep_merge(LIGHT, 'runtime' => {'frankenphp' => {'enabled' => true}}))), 'checksum ignores the schedule script')

# A processor's schedule settings still apply: off, suspended, deadline.
tuned = render(deep_merge(LIGHT, 'processors' => [
  {'name' => 'Scheduler', 'queue' => 'scheduler', 'scheduler' => {'activeDeadlineSeconds' => 60}},
  {'name' => 'Start', 'queue' => 'start', 'scheduler' => {'enabled' => false}},
  {'name' => 'Gateway', 'queue' => 'gateway', 'scheduler' => {'suspend' => true}},
  {'name' => 'Long Processor', 'queue' => 'long-processor', 'enabled' => false},
  {'name' => 'Logging', 'queue' => 'logging', 'extraEnv' => [{'name' => 'FIXTURE', 'value' => 'logging'}]}
]))
tuned_progs = programs(tuned)
check(%w[queue schedule-logging schedule-scheduler], tuned_progs.keys.sort, 'programs with tuned processors')
check('/bin/sh /etc/patchworks/schedule.sh 60', tuned_progs['schedule-scheduler']['command'], 'per-processor deadline')
assert(tuned_progs['queue']['command'].include?('--queue=scheduler,start,gateway,logging '), "queues with a disabled processor: #{tuned_progs['queue']['command']}")
check('logging', env(find(tuned, 'Deployment', 'patchworks-processor'))['FIXTURE'], 'processor extraEnv in the shared pod')

# Schedules off: CronJobs as in standard, worker runs queues only.
cron = render(deep_merge(LIGHT, 'lightweight' => {'worker' => {'schedules' => false}}))
check(names(standard, 'CronJob'), names(cron, 'CronJob'), 'CronJobs with schedules off')
check(%w[queue queue-slow], programs(cron).keys.sort, 'programs with schedules off')

# Web not combined: separate gateway and start Deployments, no APP_DOMAINS.
apart = render(deep_merge(LIGHT, 'lightweight' => {'web' => {'combine' => false}}))
assert(find(apart, 'Deployment', 'patchworks-start'), 'start Deployment missing with combine off')
assert(!env(find(apart, 'Deployment', 'patchworks-gateway')).key?('APP_DOMAINS'), 'APP_DOMAINS with combine off')
check(find(standard, 'Service', 'patchworks-start'), find(apart, 'Service', 'patchworks-start'), 'start Service with combine off')

# Start's own env reaches the combined pod.
start_env = render(deep_merge(LIGHT, 'web' => {'start' => {'extraEnv' => [{'name' => 'CALLBACK_RATE_LIMIT', 'value' => '9'}]}}))
check('9', env(find(start_env, 'Deployment', 'patchworks-gateway'))['CALLBACK_RATE_LIMIT'], 'start extraEnv on the combined pod')

# With mono allCompanies the left-out processor and schedules stay out.
mono = render(deep_merge(LIGHT, 'workers' => {'type' => 'mono', 'mono' => {'allCompanies' => {'enabled' => true}}}))
mono_progs = programs(mono)
check(%w[queue queue-slow schedule-long-processor schedule-medium-processor schedule-scheduler schedule-start], mono_progs.keys.sort, 'programs with allCompanies')
assert(mono_progs['queue']['command'].include?('--queue=scheduler,start,gateway,logging '), "queues with allCompanies: #{mono_progs['queue']['command']}")

# Refusals.
fails(deep_merge(LIGHT, 'processorDeployments' => {'autoscaling' => {'enabled' => true}}), 'cannot autoscale per queue')
fails(deep_merge(LIGHT, 'web' => {'start' => {'namespace' => 'elsewhere'}}), 'lightweight.web.combine needs web.gateway and web.start in one namespace')
render(deep_merge(LIGHT, 'web' => {'start' => {'namespace' => 'elsewhere'}}, 'lightweight' => {'web' => {'combine' => false}}))

puts 'lightweight_profile_test.rb: ok'
