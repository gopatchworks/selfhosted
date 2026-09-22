# Run: ruby tests/workload_naming_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def render(values)
  Tempfile.create(['workload-naming', '.yaml']) do |file|
    file.write(YAML.dump(values)); file.flush
    output, error, status = Open3.capture3('helm', 'template', 'naming', CHART,
      '--namespace', 'workers', '--set', 'credentials.autoGenerate=false',
      '--set', 'pusher.existingSecret.name=fixture-pusher',
      '--set', 'app.existingSecret.name=fixture-app',
      '--set', 'passport.existingSecret.name=fixture-passport', '-f', file.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def fixture(key = 'manual-payload')
  {'fullnameOverride' => "workers-#{key}", 'processorDeployments' => {'enabled' => false},
   'workers' => {'type' => 'microservice', 'microservices' => {'_default' => {'enabled' => false},
     key => {'enabled' => true, 'name' => key, 'domain' => key}},
     'autoscaling' => {'enabled' => true, 'keda' => {'rabbitmq' => {'enabled' => true,
       'host' => 'http://broker:15672/', 'existingSecret' => {'name' => 'rabbitmq-auth'}}}}}}
end
def verify(docs)
  deployments = docs.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers').any? { |c| %w[workers processor].include?(c['name']) } }
  scalers = docs.select { |d| %w[ScaledObject HorizontalPodAutoscaler].include?(d['kind']) }
  assert(deployments.size == scalers.size, 'Each enabled workload needs exactly one scaler')
  scalers.each do |scaler|
    namespace = scaler.dig('metadata', 'namespace')
    deployment = deployments.find { |d| d.dig('metadata', 'name') == scaler.dig('spec', 'scaleTargetRef', 'name') && d.dig('metadata', 'namespace') == namespace }
    assert(deployment, 'Scaler points to a missing Deployment')
    cm = deployment.dig('spec', 'template', 'spec', 'volumes').find { |v| v['name'] == 'supervisord-conf' }.dig('configMap', 'name')
    assert(docs.any? { |d| d['kind'] == 'ConfigMap' && d.dig('metadata', 'name') == cm && d.dig('metadata', 'namespace') == namespace }, 'Supervisord ConfigMap reference broken')
    assert(cm.length <= 63, 'Supervisord name exceeds the name budget')
    next unless scaler['kind'] == 'ScaledObject'
    hpa = scaler.dig('spec', 'advanced', 'horizontalPodAutoscalerConfig', 'name')
    assert(hpa && hpa.length <= 63 && hpa.match?(/\A[a-z0-9]([-a-z0-9]*[a-z0-9])?\z/), 'KEDA HPA name invalid')
    scaler.dig('spec', 'triggers').each do |trigger|
      name = trigger.dig('authenticationRef', 'name')
      assert(docs.any? { |d| d['kind'] == 'TriggerAuthentication' && d.dig('metadata', 'name') == name && d.dig('metadata', 'namespace') == namespace }, 'Authentication reference broken')
    end
  end
  identities = docs.select { |d| %w[Deployment ScaledObject TriggerAuthentication HorizontalPodAutoscaler].include?(d['kind']) }.map do |d|
    name = d.dig('metadata', 'name')
    assert(name.length <= 63, "#{d['kind']} name too long: #{name}")
    [d['kind'], d.dig('metadata', 'namespace'), name]
  end
  assert(identities.uniq == identities, 'Resource names collide')
  deployments
end

v = fixture
docs = render(v)
assert(verify(docs).map { |d| d.dig('metadata', 'name') } == ['workers-manual-payload'], 'Duplicated microservice name remains')
assert(docs.any? { |d| d['kind'] == 'ScaledObject' && d.dig('metadata', 'name') == 'workers-manual-payload-autoscaler' }, 'ScaledObject base name differs')
assert(docs.any? { |d| d['kind'] == 'TriggerAuthentication' && d.dig('metadata', 'name') == 'workers-manual-payload-autoscaler-rabbitmq' }, 'TriggerAuthentication base name differs')

v['fullnameOverride'] += '-shop'
v['workers']['hub'] = {'enabled' => false}
v['workers']['companies'] = [{'name' => 'shop', 'namespace' => 'company'}]
assert(verify(render(v)).map { |d| d.dig('metadata', 'name') } == ['workers-manual-payload-shop'], 'Duplicated company service name remains')

v = fixture
v['fullnameOverride'] = 'workers'
v['workers']['type'] = 'standalone'
assert(verify(render(v)).map { |d| d.dig('metadata', 'name') } == ['workers'], 'Duplicated standalone name remains')

v = fixture
v['fullnameOverride'] = 'processor-scheduler'
v['workers']['enabled'] = false
v['processorDeployments'] = {'enabled' => true, 'autoscaling' => {'enabled' => true}}
v['processors'] = [{'queue' => 'scheduler'}]
assert(verify(render(v)).map { |d| d.dig('metadata', 'name') } == ['processor-scheduler'], 'Duplicated processor name remains')
v['fullnameOverride'] = 'processor-medium'
v['processors'] = [{'queue' => 'medium-processor', 'deploymentName' => 'processor-medium'}]
assert(verify(render(v)).map { |d| d.dig('metadata', 'name') } == ['processor-medium'], 'Processor identity must be independent of its queue name')

# KEDA adds nine characters to the ScaledObject name unless explicitly named.
[35, 36].each do |length|
  docs = render(fixture('a' * length))
  verify(docs)
  scaler = docs.find { |d| d['kind'] == 'ScaledObject' }
  default_hpa = "keda-hpa-#{scaler.dig('metadata', 'name')}"
  actual_hpa = scaler.dig('spec', 'advanced', 'horizontalPodAutoscalerConfig', 'name')
  assert(actual_hpa.length <= 63, "HPA boundary #{length}: #{actual_hpa} (#{actual_hpa.length}), default #{default_hpa} (#{default_hpa.length})")
  assert((actual_hpa == default_hpa) == (length == 35), 'Only overlength HPA names should change')
end

# Names with a shared truncated prefix must remain distinct and deterministic.
v = fixture
v['fullnameOverride'] = 'tenant-' + 'a' * 43
v['workers']['microservices'] = {'_default' => {'enabled' => false}}
%w[a b].each { |suffix| v['workers']['microservices']['service-' + 'x' * 25 + suffix] = {'enabled' => true} }
docs = render(v)
assert(verify(docs).size == 2, 'Long-name fixture did not produce two workers')
assert(docs == render(v), 'Names must be deterministic across renders')
v['workers']['autoscaling']['provider'] = 'hpa'
v['workers']['autoscaling']['hpa'] = {'cpu' => 60}
v['workers']['resources'] = {'requests' => {'cpu' => '100m', 'memory' => '128Mi'}}
verify(render(v))
puts 'Workload naming: deduplication, references, length boundaries and collision resistance passed'
