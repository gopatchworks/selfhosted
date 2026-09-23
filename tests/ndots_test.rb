# Run: ruby tests/ndots_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'

def assert(value, message)
  raise message unless value
end

def render(chart, values = {})
  Tempfile.create(['ndots', '.yaml']) do |file|
    file.write(YAML.dump(values))
    file.flush
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', File.expand_path("../charts/patchworks-#{chart}", __dir__),
      '--namespace', 'install', '-f', file.path
    )
    raise error unless status.success?

    YAML.load_stream(output).compact.map do |document|
      spec = case document['kind']
             when 'Deployment', 'Job'
               document.dig('spec', 'template', 'spec')
             when 'CronJob'
               document.dig('spec', 'jobTemplate', 'spec', 'template', 'spec')
             end
      [document.dig('metadata', 'labels', 'app.kubernetes.io/component'), spec] if spec
    end.compact.to_h
  end
end

def check(spec, expected, name)
  assert(spec, "Missing pod template: #{name}")
  if expected.nil?
    assert(!spec.key?('dnsConfig'), "#{name}: unset ndots emitted dnsConfig")
  else
    assert(spec['dnsConfig'] == {'options' => [{'name' => 'ndots', 'value' => expected.to_s}]},
           "#{name}: expected ndots #{expected}, got #{spec['dnsConfig'].inspect}")
  end
end

def expect(pods, name, expected)
  check(pods[name], expected, name)
end

app_options = {
  'dashboard' => {'enabled' => true},
  'seeds' => {
    'fabric' => {'enabled' => true}, 'core' => {'enabled' => true},
    'tenant' => {'companyName' => 'Fixture', 'adminName' => 'Admin', 'adminEmail' => 'admin@example.com'}
  }
}
infra_options = {
  'kubefaas' => {'enabled' => true, 'builder' => {'tls' => {
    'mode' => 'existingSecret', 'existingSecret' => {'serverTls' => 'server-tls', 'clientTls' => 'client-tls'}
  }}},
  'fabric' => {'mysql' => {'enabled' => true}, 'redis' => {'enabled' => true}},
  'mysql' => {'databases' => ['extra']}
}

# Check every chart-owned pod template, including optional workloads and hooks.
%w[standalone microservice mono].each do |mode|
  options = app_options.merge('workers' => {'type' => mode, 'companies' => [{'name' => 'shop'}]})
  [nil, '', 0, 2, '3'].each do |value|
    pods = render('app', options.merge('ndots' => value))
    assert(pods.length > 10, 'Application fixture did not render its workloads')
    pods.each { |name, spec| check(spec, value == '' ? nil : value, "#{mode}/#{name}") }
  end
end
[nil, '', 0, 2, '3'].each do |value|
  pods = render('infra', infra_options.merge('ndots' => value))
  assert(pods.length >= 13, 'Infrastructure fixture did not render its workloads')
  pods.each { |name, spec| check(spec, value == '' ? nil : value, name) }
end

# Per-service settings also work without a top-level value and do not leak.
pods = render('app', 'web' => {'gateway' => {'ndots' => 0}})
pods.each { |name, spec| check(spec, name == 'gateway' ? 0 : nil, name) }

pods = render('app', app_options.merge(
  'ndots' => 5,
  'web' => {'ndots' => 2, 'gateway' => {'ndots' => 0}, 'start' => {'ndots' => ''}},
  'fabric' => {'ndots' => 3, 'migrations' => {'ndots' => 0}},
  'dashboard' => {'enabled' => true, 'ndots' => '0'},
  's3Manager' => {'ndots' => 4},
  'migrations' => {'ndots' => 2},
  'workers' => {'ndots' => 4},
  'processorDeployments' => {'ndots' => 3},
  'scheduler' => {'ndots' => 2},
  'processors' => [
    {'name' => 'Inherited', 'queue' => 'inherited'},
    {'name' => 'Override', 'queue' => 'override', 'ndots' => 1, 'scheduler' => {'ndots' => 0}}
  ]
))
{
  'gateway' => 0, 'start' => 2, 'fabric' => 3, 'fabric-migrations' => 0,
  'core-migrations' => 2, 'dashboard' => 0, 's3-manager' => 4, 'workers' => 4,
  'processor-inherited' => 3, 'processor-override' => 1,
  'inherited-scheduler' => 2, 'override-scheduler' => 0
}.each { |name, value| expect(pods, name, value) }

# Every worker mode supports section, hub and company overrides, including zero.
%w[standalone microservice mono].each do |mode|
  workers = {
    'type' => mode, 'ndots' => 2,
    'companies' => [{'name' => 'shop', 'ndots' => 0}, {'name' => 'inherited'}],
    'mono' => {'ndots' => 3},
    'microservices' => {'_default' => {'ndots' => 3}, 'batch' => {'ndots' => 4}}
  }
  pods = render('app', 'ndots' => 5, 'workers' => workers)
  prefix = mode == 'microservice' ? 'workers-batch' : 'workers'
  inherited = {'standalone' => 2, 'microservice' => 4, 'mono' => 3}[mode]
  expect(pods, prefix, inherited)
  expect(pods, "#{prefix}-shop", 0)
  expect(pods, "#{prefix}-inherited", inherited)
  expect(pods, 'workers-cache', 3) if mode == 'microservice'

  workers['hub'] = {'ndots' => 0}
  workers['companies'][0]['microservices'] = {'batch' => {'ndots' => 1}}
  pods = render('app', 'ndots' => 5, 'workers' => workers)
  expect(pods, prefix, 0)
  expect(pods, "#{prefix}-shop", mode == 'microservice' ? 1 : 0)
  expect(pods, "#{prefix}-inherited", inherited)
end

pods = render('infra', infra_options.merge(
  'ndots' => 5,
  'mysql' => {'ndots' => 0, 'databases' => ['extra']},
  'redis' => {'ndots' => 1}, 'rabbitmq' => {'ndots' => 2},
  'elasticsearch' => {'ndots' => 3}, 's3' => {'ndots' => 4},
  'fabric' => {'ndots' => 2, 'mysql' => {'enabled' => true, 'ndots' => 0}, 'redis' => {'enabled' => true}},
  'kubefaas' => infra_options['kubefaas'].merge('ndots' => 3, 'controller' => {'ndots' => 0}),
  'pusher' => {'ndots' => 2}, 'soketi' => {'ndots' => 0},
  'credentials' => {'ndots' => 3, 'generator' => {'ndots' => 0}}
))
{
  'mysql' => 0, 'mysql-setup' => 0, 'redis' => 1, 'rabbitmq' => 2,
  'elasticsearch' => 3, 's3' => 4, 's3-setup' => 4,
  'fabric-mysql' => 0, 'fabric-redis' => 2,
  'kubefaas-controller' => 0, 'kubefaas-builder' => 3, 'kubefaas-redis' => 3,
  'soketi' => 0, 'credential-generator' => 0
}.each { |name, value| expect(pods, name, value) }

pods = render('app', app_options.merge(
  'ndots' => 5,
  'credentials' => {'ndots' => 3, 'generator' => {'ndots' => 2}},
  'app' => {'ndots' => 0}, 'passport' => {'ndots' => 1}, 'pusher' => {'ndots' => 4},
  'rabbitmq' => {'ndots' => 2, 'topology' => {'ndots' => 0}},
  'seeds' => app_options['seeds'].merge('ndots' => 3, 'core' => {'enabled' => true, 'ndots' => 0},
                                      'tenant' => app_options['seeds']['tenant'].merge('ndots' => 1))
))
{
  'app-keygen' => 0, 'passport-keygen' => 1, 'pusher-auth-generator' => 4,
  'rabbitmq-topology' => 0, 'fabric-seeds' => 3, 'core-seeds' => 0,
  'fabric-company-seed' => 1, 'tenant-database' => 1, 'tenant-admin-keygen' => 1
}.each { |name, value| expect(pods, name, value) }

puts 'Pod DNS: unset defaults, global inheritance, section/service/company overrides and zero values passed'
