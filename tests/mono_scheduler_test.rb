# Run: ruby tests/mono_scheduler_test.rb (requires Helm; no cluster access).
require 'yaml'
require 'open3'
chart = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(chart, *settings)
  args = ['helm', 'template', 'test', chart, '--namespace', 'install', '--set', 'workers.type=mono', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '--set', 'workers.namespace=hub', '--set', 'workers.companies[0].name=shop', '--set', 'workers.companies[0].namespace=customer']
  settings.each { |s| args += ['--set', s] }
  output, error, status = Open3.capture3(*args)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def worker(docs, component)
  docs.find { |r| r['kind'] == 'Deployment' && r.dig('spec','template','metadata','labels','app.kubernetes.io/name') == component }
end
def env(deployment)
  deployment.dig('spec','template','spec','containers').first['env'].to_h { |e| [e['name'], e] }
end
baseline = render(chart)
hub = worker(baseline, 'workers')
company = worker(baseline, 'workers-shop')
assert(env(hub).dig('SCHEDULER_MODE','value') == 'kubernetes', 'hub default must schedule')
assert(env(company).dig('SCHEDULER_MODE','value') == 'disabled', 'dedicated company worker must not schedule the catalogue')
assert(env(company).dig('WORKER_ENABLE_DURABLE_EXECUTION','value') == 'true', 'company worker needs durable execution')
assert(env(hub).dig('SCHEDULER_POD_UID','valueFrom','fieldRef','fieldPath') == 'metadata.uid', 'pod UID must use downward API')
runtime_name = env(hub).dig('SCHEDULER_RUNTIME_CONFIG','value')
assert(!baseline.any? { |r| r['kind'] == 'ConfigMap' && r.dig('metadata','name') == runtime_name }, 'Helm must not own runtime state')
binding = baseline.find { |r| r['kind'] == 'RoleBinding' && r.dig('metadata','name').end_with?('-scheduler') }
assert(binding.dig('metadata','namespace') == 'hub', 'binding belongs in coordination namespace')
assert(binding['subjects'].first['name'] == hub.dig('spec','template','spec','serviceAccountName'), 'bind actual shared service account')
assert(binding['subjects'].first['namespace'] == 'hub', 'service account namespace mismatch')
changed = render(chart, 'workers.mono.scheduler.shards=5')
assert(hub.dig('spec','template') == worker(changed,'workers').dig('spec','template'), 'shard changes must not roll the hub')
assert(company.dig('spec','template') == worker(changed,'workers-shop').dig('spec','template'), 'shard changes must not roll company workers')
%w[standalone disabled].each do |mode|
  docs = render(chart, "workers.mono.scheduler.mode=#{mode}")
  assert(env(worker(docs,'workers')).dig('SCHEDULER_MODE','value') == mode, 'mode mismatch')
  assert(worker(docs,'workers').dig('spec','template','spec','automountServiceAccountToken') == false, 'local/disabled mode must not mount Kubernetes credentials')
  assert(!docs.any? { |r| %w[Role RoleBinding].include?(r['kind']) && r.dig('metadata','name').end_with?('-scheduler') }, 'local/disabled mode must omit scheduler permissions')
end
puts 'Selfhosted scheduler: defaults, modes, namespaces, company isolation and live shard configuration passed'
