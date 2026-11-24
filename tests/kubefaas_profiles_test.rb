# Run: ruby tests/kubefaas_profiles_test.rb (Helm required; no cluster access).
#
# The KubeFaaS controller keeps functions and profiles as cluster-scoped custom
# resources (kubefaas.patchworks.io/v1alpha1). The chart ships their CRDs in
# crds/ and renders one Profile per kubefaas.functions.profiles key: default,
# test and autoscaling, which Core picks by name. A Profile without resources
# gets kubefaas.functions.profile.resources, which the lightweight profile
# replaces. Every rendered spec must fit the CRD's schema, because the API
# server drops fields it doesn't know and rejects wrong types.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render(values)
  Tempfile.create(['kubefaas-profiles', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    # Helm rejects the "--- {}" YAML.dump writes for an empty hash.
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *(values.empty? ? [] : ['-f', f.path]))
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
KUBEFAAS = {'kubefaas' => {'enabled' => true}}.freeze
def kubefaas(values = {})
  deep_merge(KUBEFAAS, {'kubefaas' => values})
end
def profiles(docs)
  docs.select { |d| d['kind'] == 'Profile' }.to_h { |d| [d.dig('metadata', 'name'), d] }
end

# The CRDs: KubeFaaS's own, cluster-scoped, in the group the controller's RBAC names.
CRDS = Dir[File.join(CHART, 'crds', '*.yaml')].sort.to_h do |path|
  crd = YAML.safe_load(File.read(path))
  check('CustomResourceDefinition', crd['kind'], "#{File.basename(path)} kind")
  [crd.dig('spec', 'names', 'kind'), crd]
end
check(%w[Function Profile], CRDS.keys.sort, 'CRD kinds')
CRDS.each do |kind, crd|
  check("#{crd.dig('spec', 'names', 'plural')}.kubefaas.patchworks.io", crd.dig('metadata', 'name'), "#{kind} CRD name")
  check('Cluster', crd.dig('spec', 'scope'), "#{kind} CRD scope")
  check([['v1alpha1', true, true]], crd.dig('spec', 'versions').map { |v| [v['name'], v['served'], v['storage']] }, "#{kind} CRD versions")
end
PROFILE_SCHEMA = CRDS['Profile'].dig('spec', 'versions', 0, 'schema', 'openAPIV3Schema', 'properties', 'spec')

# Every key in value is in schema, with its type: what server-side validation
# would keep and accept.
def fits(schema, value, path)
  return if schema['x-kubernetes-preserve-unknown-fields']
  if schema['x-kubernetes-int-or-string']
    assert(value.is_a?(Integer) || value.is_a?(String), "#{path}: #{value.inspect} is not an int or string")
    return
  end
  case schema['type']
  when 'object'
    assert(value.is_a?(Hash), "#{path}: #{value.inspect} is not an object")
    value.each do |key, child|
      child_schema = schema.dig('properties', key) || (schema['additionalProperties'].is_a?(Hash) ? schema['additionalProperties'] : nil)
      assert(child_schema, "#{path}.#{key} is not in the Profile CRD, so the API server would drop it")
      fits(child_schema, child, "#{path}.#{key}")
    end
  when 'array'
    assert(value.is_a?(Array), "#{path}: #{value.inspect} is not an array")
    value.each_with_index { |item, i| fits(schema['items'], item, "#{path}[#{i}]") }
  when 'string'
    assert(value.is_a?(String), "#{path}: #{value.inspect} is not a string")
    assert(schema['enum'].nil? || schema['enum'].include?(value), "#{path}: #{value.inspect} is not one of #{schema['enum']}")
  when 'integer'
    assert(value.is_a?(Integer), "#{path}: #{value.inspect} is not an integer")
    assert(schema['minimum'].nil? || value >= schema['minimum'], "#{path}: #{value} is below #{schema['minimum']}")
    assert(schema['maximum'].nil? || value <= schema['maximum'], "#{path}: #{value} is above #{schema['maximum']}")
  when 'boolean'
    assert([true, false].include?(value), "#{path}: #{value.inspect} is not a boolean")
  else
    raise "#{path}: schema type #{schema['type'].inspect} is not handled"
  end
end

# KubeFaaS off: no Profiles. The CRDs stay in crds/, which Helm applies on a
# first install whatever kubefaas.enabled says.
check({}, profiles(render({})), 'Profiles with KubeFaaS off')

standard = render(KUBEFAAS)
rendered = profiles(standard)
check(%w[autoscaling default test], rendered.keys.sort, 'Profile names')
rendered.each do |name, profile|
  check('kubefaas.patchworks.io/v1alpha1', profile['apiVersion'], "#{name} apiVersion")
  check(nil, profile.dig('metadata', 'namespace'), "#{name} namespace (cluster-scoped)")
  check({'app.kubefaas.io/profile' => 'true', 'app.kubefaas.io/profile-name' => name},
        profile.dig('metadata', 'labels').slice('app.kubefaas.io/profile', 'app.kubefaas.io/profile-name'), "#{name} labels")
  fits(PROFILE_SCHEMA, profile['spec'], "#{name}.spec")
end
FUNCTION = {'requests' => {'cpu' => '10m', 'memory' => '128M'}, 'limits' => {'cpu' => '', 'memory' => ''}}.freeze
check({'enableServiceLinks' => false, 'resources' => FUNCTION}, rendered['default']['spec'], 'default profile')
check({'enableServiceLinks' => false, 'resources' => FUNCTION, 'shutdownAfterInactive' => '30m', 'autoDeleteAfter' => '30m'},
      rendered['test']['spec'], 'test profile')
autoscaling = rendered['autoscaling']['spec']
check({'requests' => {'cpu' => '125m', 'memory' => '256M'}, 'limits' => {'cpu' => '', 'memory' => ''}}, autoscaling['resources'], 'autoscaling resources')
check({'enabled' => true, 'minReplicas' => 2, 'maxReplicas' => 10, 'targetCPUUtilizationPercentage' => 80, 'disruptionBudget' => {'maxUnavailable' => 1}},
      autoscaling['autoscaler'], 'autoscaler')
check(%w[2h 36h], autoscaling.values_at('shutdownAfterInactive', 'autoDeleteAfter'), 'autoscaling lifetimes')

# The ConfigMap the controller read before it kept profiles as resources is gone.
assert(standard.none? { |d| d['kind'] == 'ConfigMap' && d.dig('metadata', 'name') == 'profile-default' }, 'profile-default ConfigMap still rendered')

# The controller makes a PodDisruptionBudget for a profile's disruptionBudget.
role = standard.find { |d| d['kind'] == 'ClusterRole' && d.dig('metadata', 'name') == 'patchworks-kubefaas-controller' }
pdb = role['rules'].find { |r| r['apiGroups'] == ['policy'] }
check({'apiGroups' => ['policy'], 'resources' => ['poddisruptionbudgets'], 'verbs' => %w[get list watch create update patch delete]}, pdb, 'PodDisruptionBudget rule')
%w[functions profiles].each do |plural|
  assert(role['rules'].any? { |r| r['apiGroups'] == ['kubefaas.patchworks.io'] && r['resources'].include?(plural) }, "ClusterRole misses #{plural}")
end

# The lightweight profile's function request reaches every profile that
# doesn't set its own resources.
light = profiles(render(deep_merge(KUBEFAAS, 'profile' => 'lightweight')))
LIGHT_FUNCTION = {'requests' => {'cpu' => '10m', 'memory' => '16M'}, 'limits' => {'cpu' => '', 'memory' => ''}}.freeze
check([LIGHT_FUNCTION, LIGHT_FUNCTION], light.values_at('default', 'test').map { |p| p.dig('spec', 'resources') }, 'lightweight default and test resources')
check('256M', light.dig('autoscaling', 'spec', 'resources', 'requests', 'memory'), 'lightweight autoscaling request')
light.each { |name, profile| fits(PROFILE_SCHEMA, profile['spec'], "lightweight #{name}.spec") }

# Values: override a field, drop a profile, add one, set resources as a map.
custom = profiles(render(kubefaas('functions' => {
  'profile' => {'resources' => {'requests' => {'cpu' => '20m', 'memory' => '64M'}}},
  'profiles' => {
    'autoscaling' => {'autoscaler' => {'maxReplicas' => 4}},
    'test' => nil,
    'shared' => {'shared' => {'enabled' => true, 'runtimes' => %w[php node], 'minReplicas' => 1, 'maxReplicas' => 5}}
  }
})))
check(%w[autoscaling default shared], custom.keys.sort, 'Profiles with test dropped and shared added')
check(4, custom.dig('autoscaling', 'spec', 'autoscaler', 'maxReplicas'), 'overridden maxReplicas')
check(2, custom.dig('autoscaling', 'spec', 'autoscaler', 'minReplicas'), 'kept minReplicas')
check({'requests' => {'cpu' => '20m', 'memory' => '64M'}}, custom.dig('default', 'spec', 'resources'), 'map resources')
check({'requests' => {'cpu' => '20m', 'memory' => '64M'}}, custom.dig('shared', 'spec', 'resources'), 'added profile resources')
custom.each { |name, profile| fits(PROFILE_SCHEMA, profile['spec'], "custom #{name}.spec") }

# The controller watches its Profiles, so a profile edit restarts neither it nor
# the builder; a change it reads from its environment does.
def config_checksums(docs)
  docs.select { |d| d['kind'] == 'Deployment' && %w[patchworks-kubefaas-controller patchworks-kubefaas-builder].include?(d.dig('metadata', 'name')) }
      .to_h { |d| [d.dig('metadata', 'name'), d.dig('spec', 'template', 'metadata', 'annotations', 'checksum/config')] }
end
base = config_checksums(standard)
check(2, base.values.compact.length, 'config checksums')
check(base, config_checksums(render(kubefaas('functions' => {'profiles' => {'autoscaling' => {'autoscaler' => {'maxReplicas' => 4}}}}))), 'checksums after a profile edit')
check(base, config_checksums(render(kubefaas('functions' => {'profile' => {'resources' => "requests:\n  memory: 64M\n"}}))), 'checksums after a profile.resources edit')
assert(config_checksums(render(kubefaas('functions' => {'namespaceCount' => 3}))).values.none? { |c| base.value?(c) }, 'namespaceCount no longer restarts the controller')

# The schema check catches a field the CRD doesn't have.
begin
  fits(PROFILE_SCHEMA, {'enableServiceLinks' => 'false'}, 'spec')
  raise 'a string enableServiceLinks passed the schema check'
rescue RuntimeError => e
  raise unless e.message.include?('is not a boolean')
end
begin
  fits(PROFILE_SCHEMA, {'replicas' => 2}, 'spec')
  raise 'an unknown field passed the schema check'
rescue RuntimeError => e
  raise unless e.message.include?('would drop it')
end

puts 'kubefaas profiles render tests passed'
