# Run: ruby tests/core_scheduler_test.rb (Helm required; no cluster access).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end

def render(values = {})
  Tempfile.create(['core-scheduler', '.yaml']) do |file|
    file.write(JSON.generate(values))
    file.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART,
      '--namespace', 'install', '--set', 'credentials.autoGenerate=false',
      '--set', 'pusher.existingSecret.name=fixture-pusher',
      '--set', 'app.existingSecret.name=fixture-app',
      '--set', 'passport.existingSecret.name=fixture-passport', '-f', file.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end

def component(documents, kind, name)
  matches = documents.select { |d| d['kind'] == kind && d.dig('metadata', 'labels', 'app.kubernetes.io/component') == name }
  assert(matches.length == 1, "Expected one #{kind}/#{name}, got #{matches.length}")
  matches.first
end

def check_scheduler(documents, segments)
  deployment = component(documents, 'Deployment', 'processor-scheduler')
  cron = component(documents, 'CronJob', 'scheduler-scheduler')
  containers = [deployment.dig('spec', 'template', 'spec', 'containers', 0),
                cron.dig('spec', 'jobTemplate', 'spec', 'template', 'spec', 'containers', 0)]
  containers.each do |container|
    env = container['env'].to_h { |item| [item['name'], item['value']] }
    assert(env['APP_DOMAIN'] == 'scheduler', 'Wrong scheduler domain')
    assert(env['REDIS_QUEUE'] == 'scheduler', 'Nested jobs would inherit the shared default queue')
    assert(env['FLOW_SCHEDULER_SEGMENTS'] == segments.to_s, 'Segments did not reach worker and cron')
  end
  assert(cron.dig('spec', 'schedule') == '*/1 * * * *', 'Cron must run once per minute')
  assert(cron.dig('spec', 'concurrencyPolicy') == 'Forbid', 'Cron overlaps itself')
  assert(containers.last['args'].include?('schedule:run'), 'Cron does not run Laravel scheduler')
  conf = component(documents, 'ConfigMap', 'processor-scheduler').dig('data', 'supervisord.conf')
  assert(conf.include?('--queue=scheduler'), 'Worker does not consume scheduler queue')
  topology = component(documents, 'ConfigMap', 'rabbitmq-topology').dig('data', 'topology.yaml')
  queue = YAML.safe_load(topology)['queues'].find { |q| q['name'] == 'scheduler' }
  assert(queue && queue['durable'] && queue['type'] == 'quorum', 'Missing durable quorum scheduler queue')
  %w[start medium-processor long-processor].each { |queue| component(documents, 'CronJob', "#{queue}-scheduler") }
end

baseline = render
check_scheduler(baseline, 1)
processors = YAML.load_file(File.join(CHART, 'values.yaml'))['processors']
scheduler = processors.find { |p| p['queue'] == 'scheduler' }
scheduler['extraEnv'].find { |e| e['name'] == 'FLOW_SCHEDULER_SEGMENTS' }['value'] = '4'
changed = render('processors' => processors)
check_scheduler(changed, 4)
assert(baseline.count { |d| d['kind'] == 'CronJob' } == changed.count { |d| d['kind'] == 'CronJob' }, 'Segments must not create more CronJobs')
scheduler['enabled'] = false
disabled = render('processors' => processors)
assert(disabled.none? { |d| %w[processor-scheduler scheduler-scheduler].include?(d.dig('metadata', 'labels', 'app.kubernetes.io/component')) }, 'Disabled processor still runs')
assert(!component(disabled, 'ConfigMap', 'rabbitmq-topology').dig('data', 'topology.yaml').include?('name: "scheduler"'), 'Disabled processor still declares its queue')
puts 'Core scheduler routing, segmentation, topology and existing cron preservation passed'
