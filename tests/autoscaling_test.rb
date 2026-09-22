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
    assert(t.dig('metadata', 'mode') == 'ExpectedQueueConsumptionTime', 'Consumption-time scaling must be the RabbitMQ default')
    assert(t['metricType'] == 'Value', 'Consumption-time scaling must use a global Value target')
    assert(t.dig('metadata', 'value') == '4', 'Consumption-time target changed')
    assert(t.dig('metadata', 'unsafeSsl') == 'false', 'TLS verification must default on')
    assert(t.dig('metadata', 'activationValue') == '0', 'Consumption-time activation changed')
    assert(s.dig('spec', 'pollingInterval') == 1, 'Consumption-time scaling requires one-second polling')
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
render(v, failure: 'ExpectedQueueConsumptionTime require')
v = config
r = v['workers']['autoscaling']['keda']['rabbitmq']
r['protocol'] = 'amqp'
r['expectedQueueConsumptionTime'] = {'enabled' => false}
r['messageRate'] = {'enabled' => true}
render(v, failure: 'MessageRate and ExpectedQueueConsumptionTime require')
v = config
v['workers']['autoscaling']['keda']['pollingInterval'] = 5
render(v, failure: 'ExpectedQueueConsumptionTime requires keda.pollingInterval: 1')
v = config
r = v['workers']['autoscaling']['keda']['rabbitmq']
r['expectedQueueConsumptionTime'] = {'enabled' => false}
r['queueLength'] = {'enabled' => true, 'value' => '30', 'activationValue' => '0'}
docs = render(v)
t = docs.find { |d| d['kind'] == 'ScaledObject' }.dig('spec', 'triggers').first
assert(t.dig('metadata', 'mode') == 'QueueLength' && !t.key?('metricType'), 'Explicit QueueLength compatibility lost')
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
# Processors opt in independently, even when worker autoscaling is enabled.
docs = render(config)
processor_deployments = docs.select { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name').include?('-processor-') }
assert(!processor_deployments.empty? && processor_deployments.all? { |d| d['spec'].key?('replicas') }, 'Worker policy unexpectedly enabled processor autoscaling')
assert(docs.none? { |d| d['kind'] == 'ScaledObject' && d.dig('metadata', 'name').include?('-processor-') }, 'Processors must opt in independently')
v = config
v['workers']['enabled'] = false
v['processorDeployments'] = {'autoscaling' => {'enabled' => true, 'minReplicas' => 2, 'maxReplicas' => 9, 'keda' => {
  'pollingInterval' => 30,
  'rabbitmq' => {'expectedQueueConsumptionTime' => {'enabled' => false}, 'messageRate' => {'enabled' => true, 'value' => '12'}, 'queueLength' => {'enabled' => true, 'value' => '200'}},
  'extraTriggers' => [{'type' => 'cron', 'metadata' => {'timezone' => 'UTC', 'start' => '0 9 * * *', 'end' => '0 17 * * *', 'desiredReplicas' => '4'}}]
}}}
v['processors'] = [
  {'queue' => 'custom.queue', 'namespace' => 'processor-custom', 'processes' => 7, 'resources' => {'requests' => {'cpu' => '100m'}}},
  {'queue' => 'second', 'namespace' => 'processor-second', 'autoscaling' => {'maxReplicas' => 11, 'keda' => {'extraTriggers' => [], 'rabbitmq' => {'messageRate' => {'value' => '5'}}}}},
  {'queue' => 'fixed', 'replicas' => 5, 'autoscaling' => {'enabled' => false}},
  {'queue' => 'disabled', 'enabled' => false}
]
docs = render(v)
scalers = docs.select { |d| d['kind'] == 'ScaledObject' }
assert(scalers.size == 2, 'Processor enable/disable or worker independence failed')
scalers.each do |s|
  dep = docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name') == s.dig('spec', 'scaleTargetRef', 'name') && d.dig('metadata', 'namespace') == s.dig('metadata', 'namespace') }
  assert(dep && !dep['spec'].key?('replicas'), 'Processor scaler target/replica ownership incorrect')
  expected_queue = s.dig('metadata', 'namespace') == 'processor-custom' ? 'custom.queue' : 'second'
  rabbit = s.dig('spec', 'triggers').select { |t| t['type'] == 'rabbitmq' }
  assert(rabbit.map { |t| t.dig('metadata', 'mode') }.sort == %w[MessageRate QueueLength], 'Processor RabbitMQ modes missing')
  assert(rabbit.all? { |t| t.dig('metadata', 'queueName') == expected_queue }, 'Processor queue resolution incorrect')
  auth = docs.find { |d| d['kind'] == 'TriggerAuthentication' && d.dig('metadata', 'name') == rabbit.first.dig('authenticationRef', 'name') }
  assert(auth && auth.dig('metadata', 'namespace') == s.dig('metadata', 'namespace'), 'Processor authentication namespace incorrect')
end
custom = scalers.find { |s| s.dig('metadata', 'namespace') == 'processor-custom' }
assert(custom.dig('spec', 'scaleTargetRef', 'name') == 'patchworks-processor-custom-queue', 'Processor target slug incorrect')
assert(custom.dig('spec', 'maxReplicaCount') == 9 && custom.dig('spec', 'pollingInterval') == 30, 'Global processor policy missing')
assert(custom.dig('spec', 'triggers').any? { |t| t['type'] == 'cron' }, 'Processor cron prewarming missing')
second = scalers.find { |s| s.dig('metadata', 'namespace') == 'processor-second' }
assert(second.dig('spec', 'maxReplicaCount') == 11 && second.dig('spec', 'triggers').size == 2, 'Per-processor scalar/list overrides lost')
assert(custom.dig('spec', 'triggers').find { |t| t.dig('metadata', 'mode') == 'MessageRate' }.dig('metadata', 'value') == '12', 'Processor override leaked into sibling')
assert(docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name') == 'patchworks-processor-fixed' }.dig('spec', 'replicas') == 5, 'Fixed processor replica count lost')
assert(docs.any? { |d| d['kind'] == 'CronJob' }, 'Processor autoscaling changed scheduler selection')
# Per-queue HPA uses that queue's resource requests, not only worker defaults.
v['processors'][0]['autoscaling'] = {'provider' => 'hpa', 'hpa' => {'cpu' => 70}}
docs = render(v)
hpa, = docs.select { |d| d['kind'] == 'HorizontalPodAutoscaler' }
assert(hpa.dig('spec', 'scaleTargetRef', 'name') == 'patchworks-processor-custom-queue', 'Processor HPA target incorrect')
assert(hpa.dig('spec', 'metrics', 0, 'resource', 'target', 'averageUtilization') == 70, 'Processor HPA metric incorrect')
assert(docs.count { |d| d['kind'] == 'ScaledObject' } == 1, 'Processor emitted competing scalers')
v['processors'][0].delete('resources')
render(v, failure: 'requests.cpu')
v['processorDeployments']['enabled'] = false
assert(render(v).none? { |d| %w[ScaledObject HorizontalPodAutoscaler TriggerAuthentication].include?(d['kind']) }, 'Disabled processor Deployments still emit scalers')

puts 'Autoscaling: processors, worker modes, target ownership, HPA, KEDA, authentication, queries, inheritance and validation passed'
