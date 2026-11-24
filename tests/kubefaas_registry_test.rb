# Run: ruby tests/kubefaas_registry_test.rb (Helm required; no cluster access).
#
# kubefaas.registry.bundled: a registry inside the cluster for KubeFaaS function
# images, named localhost:<nodePort>/... The chart renders its login,
# registries.yaml for the builder and a pull Secret in every function namespace
# from one login, and gives the builder a proxy to the registry on localhost.
# kubefaas.builder.platform sets BASE_IMAGE_PLATFORM. The KubeFaaS auth Secret
# is rendered by the chart from one login, not by the credential generator.
require 'yaml'
require 'json'
require 'base64'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render_raw(values)
  Tempfile.create(['kubefaas-registry', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    # Helm rejects the "--- {}" YAML.dump writes for an empty hash.
    Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *(values.empty? ? [] : ['-f', f.path]))
  end
end
def render(values)
  output, error, status = render_raw(values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def kubefaas(values = {})
  {'kubefaas' => {'enabled' => true}.merge(values)}
end
def bundled(values = {}, kubefaas_values = {})
  kubefaas({'registry' => {'bundled' => {'enabled' => true}.merge(values)}}.merge(kubefaas_values))
end
def find(docs, kind, name, namespace = 'kubefaas')
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name && d.dig('metadata', 'namespace') == namespace }
end
def secret_data(secret)
  (secret['data'] || {}).transform_values { |v| Base64.decode64(v) }.merge(secret['stringData'] || {})
end
def pod(docs, name)
  find(docs, 'Deployment', name).dig('spec', 'template')
end
def container(docs, deployment, name)
  pod(docs, deployment).dig('spec', 'containers').find { |c| c['name'] == name }
end
REGISTRY = 'patchworks-kubefaas-image-registry'
PULL = "#{REGISTRY}-pull"
FUNCTION_NAMESPACES = %w[kubefaas kubefaas-fn-01 kubefaas-fn-02 kubefaas-fn-03 kubefaas-fn-04 kubefaas-fn-05].freeze
def registry_objects(docs)
  docs.select { |d| d.dig('metadata', 'name').to_s.start_with?(REGISTRY) }.map { |d| [d['kind'], d.dig('metadata', 'namespace'), d.dig('metadata', 'name')] }
end

# Off unless asked for, with KubeFaaS off or on.
[{}, kubefaas].each do |values|
  docs = render(values)
  check([], registry_objects(docs), "registry objects for #{values.inspect}")
  next if values.empty?
  check(nil, container(docs, 'patchworks-kubefaas-builder', 'registry-proxy'), 'proxy without the bundled registry')
  check(nil, find(docs, 'ConfigMap', 'patchworks-kubefaas-builder-env').dig('data', 'BASE_IMAGE_PLATFORM'), 'platform by default')
  check(nil, find(docs, 'ServiceAccount', 'kubefaas-function', 'kubefaas-fn-01')['imagePullSecrets'], 'function pull Secrets without the registry')
  assert(find(docs, 'Secret', 'patchworks-kubefaas-registry'), 'registries.yaml from kubefaas.registry.config missing')
end
# The bundled registry needs KubeFaaS.
check([], registry_objects(render('kubefaas' => {'registry' => {'bundled' => {'enabled' => true}}})), 'registry without KubeFaaS')

docs = render(bundled)
check([['Secret', 'kubefaas', "#{REGISTRY}-auth"]] + FUNCTION_NAMESPACES.map { |ns| ['Secret', ns, PULL] } +
      [['PersistentVolumeClaim', 'kubefaas', REGISTRY], ['Service', 'kubefaas', REGISTRY], ['Deployment', 'kubefaas', REGISTRY]],
      registry_objects(docs), 'registry objects')

# One generated login, everywhere it is used.
login = secret_data(find(docs, 'Secret', "#{REGISTRY}-auth"))
check('kubefaas', login['username'], 'generated username')
check(32, login['password'].length, 'generated password length')
registries = YAML.safe_load(secret_data(find(docs, 'Secret', 'patchworks-kubefaas-registry'))['registries.yaml'])
check([
  {'name' => 'default', 'type' => 'docker', 'url' => 'localhost:30500', 'username' => 'kubefaas', 'password' => login['password'],
   'imagePrefix' => 'localhost:30500', 'validImages' => ['functions']},
  {'name' => 'kubefaas-templates', 'type' => 'docker', 'url' => 'localhost:30500', 'username' => 'kubefaas', 'password' => login['password'],
   'imagePrefix' => 'localhost:30500', 'validImages' => ['kubefaas-templates']}
], registries, 'registries.yaml')
auth = Base64.strict_encode64("kubefaas:#{login['password']}")
FUNCTION_NAMESPACES.each do |ns|
  pull = find(docs, 'Secret', PULL, ns)
  check('kubernetes.io/dockerconfigjson', pull['type'], "pull Secret type in #{ns}")
  check({'auths' => {'localhost:30500' => {'username' => 'kubefaas', 'password' => login['password'], 'auth' => auth}}},
        JSON.parse(secret_data(pull)['.dockerconfigjson']), "pull Secret in #{ns}")
  check([{'name' => PULL}], find(docs, 'ServiceAccount', 'kubefaas-function', ns)['imagePullSecrets'], "function ServiceAccount in #{ns}")
end
check(pod(docs, REGISTRY).dig('metadata', 'annotations', 'checksum/login'),
      pod(docs, 'patchworks-kubefaas-builder').dig('metadata', 'annotations', 'checksum/registry-login'),
      'builder and registry roll on the same login')

# The registry: htpasswd from the login, the data volume, a NodePort.
registry = pod(docs, REGISTRY)
check('Recreate', find(docs, 'Deployment', REGISTRY).dig('spec', 'strategy', 'type'), 'registry strategy')
htpasswd = registry.dig('spec', 'initContainers', 0)
check([["#{REGISTRY}-auth", 'username'], ["#{REGISTRY}-auth", 'password']],
      htpasswd['env'].map { |e| [e.dig('valueFrom', 'secretKeyRef', 'name'), e.dig('valueFrom', 'secretKeyRef', 'key')] }, 'htpasswd env')
env = container(docs, REGISTRY, 'registry')['env'].to_h { |e| [e['name'], e['value']] }
check({'REGISTRY_AUTH' => 'htpasswd', 'REGISTRY_AUTH_HTPASSWD_REALM' => 'KubeFaaS', 'REGISTRY_AUTH_HTPASSWD_PATH' => '/auth/htpasswd',
       'REGISTRY_STORAGE_DELETE_ENABLED' => 'true'}, env, 'registry env')
check(REGISTRY, registry.dig('spec', 'volumes').find { |v| v['name'] == 'data' }.dig('persistentVolumeClaim', 'claimName'), 'data claim')
check('20Gi', find(docs, 'PersistentVolumeClaim', REGISTRY).dig('spec', 'resources', 'requests', 'storage'), 'volume size')
service = find(docs, 'Service', REGISTRY)
check(['NodePort', 5000, 30500], [service.dig('spec', 'type'), service.dig('spec', 'ports', 0, 'port'), service.dig('spec', 'ports', 0, 'nodePort')], 'service')

# The builder reaches the registry as localhost:<nodePort>, the name nodes pull by.
proxy = container(docs, 'patchworks-kubefaas-builder', 'registry-proxy')
check(['TCP-LISTEN:30500,fork,reuseaddr,bind=127.0.0.1', "TCP:#{REGISTRY}.kubefaas.svc.cluster.local:5000"], proxy['args'], 'proxy args')

# Another node port, a pinned login, an existing volume and the image pull
# Secrets everyone uses.
pinned = render(bundled({'nodePort' => 31000, 'auth' => {'username' => 'fn', 'password' => 'pinned'}, 'persistence' => {'existingClaim' => 'images'}})
                  .merge('image' => {'pullSecrets' => [{'name' => 'quay-credentials'}]}))
check({'username' => 'fn', 'password' => 'pinned'}, secret_data(find(pinned, 'Secret', "#{REGISTRY}-auth")), 'pinned login')
check(%w[localhost:31000 localhost:31000], YAML.safe_load(secret_data(find(pinned, 'Secret', 'patchworks-kubefaas-registry'))['registries.yaml']).map { |r| r['url'] }, 'registry urls on another port')
check(['localhost:31000'], JSON.parse(secret_data(find(pinned, 'Secret', PULL, 'kubefaas-fn-01'))['.dockerconfigjson'])['auths'].keys, 'pull Secret on another port')
check(31000, find(pinned, 'Service', REGISTRY).dig('spec', 'ports', 0, 'nodePort'), 'node port')
check('TCP-LISTEN:31000,fork,reuseaddr,bind=127.0.0.1', container(pinned, 'patchworks-kubefaas-builder', 'registry-proxy')['args'][0], 'proxy port')
check(nil, find(pinned, 'PersistentVolumeClaim', REGISTRY), 'PVC with an existing claim')
check('images', pod(pinned, REGISTRY).dig('spec', 'volumes').find { |v| v['name'] == 'data' }.dig('persistentVolumeClaim', 'claimName'), 'existing claim')
check([{'name' => 'quay-credentials'}, {'name' => PULL}], find(pinned, 'ServiceAccount', 'kubefaas-function', 'kubefaas-fn-02')['imagePullSecrets'], 'pull Secrets kept')

# Base images built under the same name as function images: one entry.
same = render(bundled({}, 'builder' => {'baseImagesRegistry' => 'default'}))
check([['default', ['functions']]], YAML.safe_load(secret_data(find(same, 'Secret', 'patchworks-kubefaas-registry'))['registries.yaml']).map { |r| [r['name'], r['validImages']] }, 'one entry')

# registries.yaml of your own and the bundled registry don't mix.
[{'config' => "- name: default\n"}, {'existingSecret' => {'name' => 'mine'}}].each do |registry_values|
  _, error, status = render_raw(kubefaas('registry' => registry_values.merge('bundled' => {'enabled' => true})))
  assert(!status.success? && error.include?('kubefaas.registry.bundled.enabled generates registries.yaml'), "#{registry_values.inspect} with the bundled registry rendered: #{error}")
end

# The builder's platform.
check('linux/arm64', find(render(kubefaas('builder' => {'platform' => 'linux/arm64'})), 'ConfigMap', 'patchworks-kubefaas-builder-env').dig('data', 'BASE_IMAGE_PLATFORM'), 'platform')

# KubeFaaS auth: one login in both copies, generated or inline, and not from the
# credential generator.
def auth_copies(docs)
  docs.select { |d| d['kind'] == 'Secret' && d.dig('metadata', 'name') == 'patchworks-kubefaas-auth' }.to_h { |d| [d.dig('metadata', 'namespace'), secret_data(d)] }
end
generated = auth_copies(docs)
check(%w[kubefaas install], generated.keys, 'auth Secret namespaces')
check(1, generated.values.uniq.length, 'auth copies match')
check([32, 32], [generated['kubefaas']['username'].length, generated['kubefaas']['password'].length], 'generated auth')
generator = docs.find { |d| d['kind'] == 'Job' && d.dig('metadata', 'name') == 'patchworks-credential-generator' }
assert(generator && !generator.to_yaml.include?('kubefaas'), 'credential generator missing or still writes KubeFaaS auth')
inline = auth_copies(render(kubefaas('auth' => {'username' => 'user', 'password' => 'pass'})))
check({'kubefaas' => {'username' => 'user', 'password' => 'pass'}, 'install' => {'username' => 'user', 'password' => 'pass'}}, inline, 'inline auth')
check({}, auth_copies(render(kubefaas('auth' => {'existingSecret' => {'name' => 'mine'}}))), 'auth with an existing Secret')
%w[controller builder].each do |component|
  check(pod(docs, "patchworks-kubefaas-#{component}").dig('metadata', 'annotations', 'checksum/auth').nil?, false, "#{component} auth checksum")
end

puts 'kubefaas registry render tests passed'
