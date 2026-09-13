# Run: ruby tests/autoscaling_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(values, failure: nil)
  Tempfile.create(['autoscaling', '.yaml']) do |f|
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
def config(type = 'standalone')
  {'workers' => {'type' => type, 'namespace' => 'hub', 'companies' => [{'name' => 'shop', 'namespace' => 'customer', 'queue' => 'orders'}], 'autoscaling' => {'enabled' => true, 'keda' => {'rabbitmq' => {'enabled' => true, 'host' => 'https://broker:15671/', 'existingSecret' => {'name' => 'scaler'}}}}}}
end
%w[standalone microservice mono].each do |type|
  v = config(type)
  docs = render(v)
  scalers = docs.select { |d| d['kind'] == 'ScaledObject' }
  assert(scalers.size >= 2, "#{type} hub and company scalers missing")
  scalers.each do |s|
    dep = docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name') == s.dig('spec', 'scaleTargetRef', 'name') && d.dig('metadata', 'namespace') == s.dig('metadata', 'namespace') }
    assert(dep && !dep['spec'].key?('replicas'), 'Scaler target or replica ownership incorrect')
    t = s.dig('spec', 'triggers').first
    assert(t.dig('metadata', 'unsafeSsl') == 'false', 'TLS verification must default on')
    assert(t.dig('metadata', 'activationValue') == '0', 'Zero activation lost')
    if s.dig('metadata', 'namespace') == 'customer'
      assert(t.dig('metadata', 'queueName') == 'orders', 'Company queue resolution incorrect')
    end
    auth = docs.find { |d| d['kind'] == 'TriggerAuthentication' && d.dig('metadata', 'name') == t.dig('authenticationRef', 'name') }
    assert(auth.dig('metadata', 'namespace') == s.dig('metadata', 'namespace'), 'Auth namespace mismatch')
  end
  v['workers']['companies'][0]['autoscaling'] = {'enabled' => false}
  docs = render(v)
  assert(docs.none? { |d| d['kind'] == 'ScaledObject' && d.dig('metadata', 'namespace') == 'customer' }, 'Explicit false override lost')
  v['workers']['autoscaling']['enabled'] = false
  assert(render(v).none? { |d| %w[ScaledObject HorizontalPodAutoscaler TriggerAuthentication].include?(d['kind']) }, 'Disabled defaults emitted scalers')
end
v = config
v['workers']['autoscaling'].merge!('provider' => 'hpa', 'hpa' => {'memory' => 75})
docs = render(v)
assert(docs.count { |d| d['kind'] == 'HorizontalPodAutoscaler' } == 2, 'HPA targets missing')
assert(docs.none? { |d| d['kind'] == 'ScaledObject' }, 'Competing controllers emitted')
v['workers']['autoscaling']['hpa'] = {'cpu' => 70}
render(v, failure: 'requests.cpu')
v = config
v['workers']['processes'] = 7
v['workers']['autoscaling']['keda']['prometheus'] = {'enabled' => true, 'serverAddress' => 'https://prometheus.example', 'query' => 'sum(rabbitmq_queue_messages{queue=__QUEUE__,namespace=__NAMESPACE__}) / __PROCESSES__', 'threshold' => '2'}
docs = render(v)
s = docs.find { |d| d['kind'] == 'ScaledObject' && d.dig('metadata', 'namespace') == 'customer' }
assert(s.dig('spec', 'triggers').size == 2, 'Combined triggers missing')
assert(s.dig('spec', 'triggers', 1, 'metadata', 'query').include?('queue="orders",namespace="customer"}) / 7'), 'Query context resolution failed')
v['workers']['autoscaling']['minReplicas'] = 0
v['workers']['autoscaling']['keda']['pausedReplicas'] = 0
docs = render(v)
assert(docs.find { |d| d['kind'] == 'ScaledObject' }.dig('metadata', 'annotations', 'autoscaling.keda.sh/paused-replicas') == '0', 'Pause zero lost')
v = config('mono')
v['workers']['autoscaling']['minReplicas'] = 0
render(v, failure: 'scheduler enabled cannot scale')
v['workers']['mono'] = {'scheduler' => {'mode' => 'disabled'}}
render(v)
v = config
v['workers']['autoscaling']['keda']['rabbitmq']['protocol'] = 'amqp'
v['workers']['autoscaling']['keda']['rabbitmq']['messageRate'] = {'enabled' => true}
render(v, failure: 'MessageRate requires')
v = config
v['workers']['autoscaling']['keda']['rabbitmq']['enabled'] = false
render(v, failure: 'at least one enabled trigger')
v = config
v['workers']['autoscaling']['maxReplicas'] = 0
render(v, failure: 'maxReplicas >= 1')
v = config('microservice')
v['workers']['microservices'] = {'_default' => {'autoscaling' => {'maxReplicas' => 4}}, 'custom' => {'name' => 'custom', 'domain' => 'custom-jobs', 'autoscaling' => {'maxReplicas' => 8}}}
v['workers']['companies'][0]['microservices'] = {'custom' => {'autoscaling' => {'maxReplicas' => 2}}}
docs = render(v)
custom = docs.select { |d| d['kind'] == 'ScaledObject' && d.dig('metadata', 'name').match?(/workers-custom(-shop)?-autoscaler$/) }
assert(custom.map { |d| d.dig('spec', 'maxReplicaCount') }.sort == [2, 8], 'Per-service/company inheritance failed: ' + custom.map { |d| [d.dig('metadata', 'name'), d.dig('spec', 'maxReplicaCount')] }.inspect)
# Native triggers and central authentication remain available without generated auth.
v = config
r = v['workers']['autoscaling']['keda']['rabbitmq']
r.delete('existingSecret')
r['authenticationRef'] = {'name' => 'central', 'kind' => 'ClusterTriggerAuthentication'}
v['workers']['hub'] = {'enabled' => false}
v['workers']['autoscaling']['keda']['extraTriggers'] = [{'type' => 'cron', 'metadata' => {'timezone' => 'UTC', 'start' => '0 9 * * *', 'end' => '0 17 * * *', 'desiredReplicas' => '3'}}]
v['workers']['autoscaling']['keda']['fallback'] = {'failureThreshold' => 3, 'replicas' => 2}
docs = render(v)
assert(docs.count { |d| d['kind'] == 'ScaledObject' } == 1, 'Disabled hub still scaled')
assert(docs.none? { |d| d['kind'] == 'TriggerAuthentication' }, 'Existing auth must not be recreated')
s = docs.find { |d| d['kind'] == 'ScaledObject' }
assert(s.dig('spec', 'fallback', 'replicas') == 2, 'Fallback lost')
assert(s.dig('spec', 'triggers').any? { |t| t.dig('authenticationRef', 'kind') == 'ClusterTriggerAuthentication' }, 'Cluster auth reference lost')
v['workers']['companies'][0]['autoscaling'] = {'keda' => {'extraTriggers' => []}}
s = render(v).find { |d| d['kind'] == 'ScaledObject' }
assert(s.dig('spec', 'triggers').size == 1, 'Empty trigger-list override must clear inherited triggers')
v = config('mono')
v['workers']['autoscaling']['enabled'] = false
v['workers']['mono'] = {'replicaCount' => 3}
v['workers']['companies'][0]['replicaCount'] = 2
docs = render(v)
assert(docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata','name') == 'patchworks-workers' }.dig('spec','replicas') == 3, 'Fixed mono replicas ignored')
assert(docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata','name') == 'patchworks-workers-shop' }.dig('spec','replicas') == 2, 'Fixed company replicas ignored')
Dir[File.join(CHART, 'docs/autoscaling/*.yaml')].each do |path|
  docs = render(YAML.load_file(path))
  assert(docs.any? { |d| %w[ScaledObject HorizontalPodAutoscaler].include?(d['kind']) }, "Example did not render scaler: #{path}")
end
puts 'Autoscaling: worker modes, target ownership, HPA, KEDA, authentication, queries, inheritance and validation passed'
