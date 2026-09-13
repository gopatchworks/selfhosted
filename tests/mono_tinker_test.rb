# Run: ruby tests/mono_tinker_test.rb (requires Helm; no cluster access).
require 'yaml'
require 'open3'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
def render(*settings)
  args = ['helm', 'template', 'test', CHART, '--namespace', 'install', '--set', 'workers.type=mono', '--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher', '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport', '--set', 'workers.namespace=hub', '--set', 'workers.companies[0].name=shop', '--set', 'workers.companies[0].namespace=customer']
  settings.each { |setting| args += ['--set', setting] }
  output, error, status = Open3.capture3(*args)
  raise error unless status.success?
  YAML.load_stream(output).compact.select { |r| r['kind'] == 'Deployment' && %w[workers workers-shop].include?(r.dig('spec', 'template', 'metadata', 'labels', 'app.kubernetes.io/name')) }
end
def assert(value, message)
  raise message unless value
end
def environments(docs)
  assert(docs.size == 2, 'expected both hub and company workers')
  docs.map { |doc| doc.dig('spec', 'template', 'spec', 'containers').first['env'].to_h { |e| [e['name'], e] } }
end
environments(render).each do |env|
  assert(env.dig('FABRIC_API_URL', 'value') == 'http://patchworks-fabric.install.svc.cluster.local:80/api/v2', 'default Fabric URL')
  assert(env.dig('CORE_API_URL', 'value') == 'http://patchworks-gateway.install.svc.cluster.local/api/v1/patchworks', 'default Core URL')
  assert(!env.key?('PATCHWORKS_API_TOKEN'), 'token must be omitted without Secret')
end
environments(render('fabric.namespace=fabric-ns', 'fabric.service.port=8081', 'web.gateway.namespace=gateway-ns', 'web.gateway.service.port=8082', 'workers.mono.tinker.existingSecret.name=tinker-secret', 'workers.mono.tinker.existingSecret.tokenKey=token')).each do |env|
  assert(env.dig('FABRIC_API_URL', 'value') == 'http://patchworks-fabric.fabric-ns.svc.cluster.local:8081/api/v2', 'Fabric namespace/port')
  assert(env.dig('CORE_API_URL', 'value') == 'http://patchworks-gateway.gateway-ns.svc.cluster.local:8082/api/v1/patchworks', 'Core namespace/port')
  assert(env.dig('PATCHWORKS_API_TOKEN', 'valueFrom', 'secretKeyRef') == {'name'=>'tinker-secret', 'key'=>'token'}, 'token Secret reference')
  assert(!env['PATCHWORKS_API_TOKEN'].key?('value'), 'token must not be plaintext')
end
environments(render('fabric.core.gatewayUrl=https://external-core.example/', 'workers.mono.tinker.fabricApiUrl=https://external-fabric.example/api/v2')).each do |env|
  assert(env.dig('FABRIC_API_URL', 'value') == 'https://external-fabric.example/api/v2', 'external Fabric URL')
  assert(env.dig('CORE_API_URL', 'value') == 'https://external-core.example/api/v1/patchworks', 'external Core fallback')
end
environments(render('workers.mono.tinker.coreApiUrl=https://override.example/custom')).each do |env|
  assert(env.dig('CORE_API_URL', 'value') == 'https://override.example/custom', 'explicit Core override')
end
puts 'Monocore tinker: API defaults, overrides, namespaces and token Secrets passed'

environments(render('workers.mono.tinker.generateApiToken=true', 'workers.mono.tinker.apiTokenUserId=7')).each do |env|
  assert(env.dig('GENERATE_API_TOKEN', 'value') == 'true', 'generation opt-in')
  assert(env.dig('API_TOKEN_USER_ID', 'value') == '7', 'explicit token user')
  assert(env.dig('REVOKE_GENERATED_API_TOKEN', 'value') == 'false', 'retain generated key by default')
  assert(!env.key?('PATCHWORKS_API_TOKEN'), 'generated key must not inject static token')
end
[
  ['workers.mono.tinker.generateApiToken=true'],
  ['workers.mono.tinker.generateApiToken=true', 'workers.mono.tinker.apiTokenUserId=7', 'workers.mono.tinker.existingSecret.name=conflict']
].each do |settings|
  failed = false
  begin
    render(*settings)
  rescue RuntimeError => error
    failed = error.message.include?('workers.mono.tinker')
  end
  assert(failed, 'invalid generation settings must fail rendering')
end

environments(render('workers.mono.tinker.generateApiToken=true', 'workers.mono.tinker.apiTokenUserId=7', 'workers.mono.tinker.revokeGeneratedApiToken=true')).each do |env|
  assert(env.dig('REVOKE_GENERATED_API_TOKEN', 'value') == 'true', 'explicit cleanup opt-in')
end
