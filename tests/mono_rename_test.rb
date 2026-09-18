# Run: ruby tests/mono_rename_test.rb (requires Helm; no cluster access).
# workers.mono.nameSuffix renames the Monocore hub. Every object named after the
# hub has to follow, and so does the label the scheduler selects its own pods by:
# a selector left on the old slug would match nothing and stop sharding silently.
require 'yaml'
require 'open3'
chart = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(chart, *settings)
  args = ['helm', 'template', 'test', chart, '--namespace', 'install', '--set', 'workers.type=mono', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '--set', 'workers.namespace=hub', '--set', 'workers.mono.serviceMonitor.enabled=true', '--set', 'workers.companies[0].name=shop', '--set', 'workers.companies[0].namespace=customer']
  settings.each { |s| args += ['--set', s] }
  output, error, status = Open3.capture3(*args)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def worker(docs, component)
  docs.find { |r| r['kind'] == 'Deployment' && r.dig('spec', 'template', 'metadata', 'labels', 'app.kubernetes.io/name') == component }
end
def env(deployment)
  deployment.dig('spec', 'template', 'spec', 'containers').first['env'].to_h { |e| [e['name'], e] }
end
def named(docs)
  docs.to_h { |r| [[r['kind'], r.dig('metadata', 'name')], r] }
end

baseline = render(chart)
hub = worker(baseline, 'workers')
assert(hub, 'the default hub must keep the historical "workers" slug')
fullname = hub.dig('metadata', 'name').sub(/-workers\z/, '')
assert(fullname != hub.dig('metadata', 'name'), 'default hub Deployment must be <fullname>-workers')

renamed = render(chart, 'workers.mono.nameSuffix=monocore')
hub_renamed = worker(renamed, 'monocore')
assert(hub_renamed, 'the hub must be selectable by its configured slug')
assert(!worker(renamed, 'workers'), 'the old hub slug must not survive a rename')

objects = named(renamed)
previous = named(baseline)
%w[Deployment Service ServiceMonitor].each do |kind|
  assert(previous.key?([kind, "#{fullname}-workers"]), "#{kind} baseline name mismatch; the fixture is wrong")
  assert(objects.key?([kind, "#{fullname}-monocore"]), "#{kind} must follow the hub slug")
  assert(!objects.key?([kind, "#{fullname}-workers"]), "#{kind} must not keep the old name")
end
%w[config topology].each do |suffix|
  assert(objects.key?(['ConfigMap', "#{fullname}-monocore-#{suffix}"]), "#{suffix} ConfigMap must follow the hub slug")
  assert(!objects.key?(['ConfigMap', "#{fullname}-workers-#{suffix}"]), "#{suffix} ConfigMap must not keep the old name")
end

# The pods the scheduler shards over are the ones the rename actually produced.
labels = hub_renamed.dig('spec', 'template', 'metadata', 'labels')
expected = "app.kubernetes.io/name=#{labels['app.kubernetes.io/name']},app.kubernetes.io/instance=#{labels['app.kubernetes.io/instance']}"
assert(env(hub_renamed).dig('SCHEDULER_POD_SELECTOR', 'value') == expected,
       'scheduler must select the pods the rename actually produces')
assert(hub_renamed.dig('spec', 'selector', 'matchLabels') == labels,
       'Deployment selector and pod labels must agree after a rename')

# Companies hang off the hub, so they take its prefix rather than a stale one.
assert(worker(renamed, 'monocore-shop'), 'companies must take the hub prefix')
assert(!worker(renamed, 'workers-shop'), 'companies must not keep the old prefix')

# A rename builds a new Deployment; it must not also roll the outgoing one.
assert(hub.dig('spec', 'template', 'metadata', 'annotations', 'checksum/config') ==
       hub_renamed.dig('spec', 'template', 'metadata', 'annotations', 'checksum/config'),
       'the slug must stay out of the config checksum')

puts 'Selfhosted mono rename: object names, company prefix and shard selection stay consistent'
