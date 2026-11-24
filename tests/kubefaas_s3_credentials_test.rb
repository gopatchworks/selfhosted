# Run: ruby tests/kubefaas_s3_credentials_test.rb (Helm required; no cluster access).
#
# The KubeFaaS builder caches function images in S3 (kubefaas.builder.cacheBucket)
# and runs in the KubeFaaS namespace. A Pod reads only Secrets in its own
# namespace, and the S3 login's Secret is in another, so the chart copies the
# login into <fullname>-kubefaas-s3 there. A login the credential generator
# creates is generated at render and handed to the generator, so both agree.
# `helm template` has no lookup, so these renders are always a fresh install.
require 'yaml'
require 'open3'
require 'tempfile'
require 'base64'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
def render(values)
  Tempfile.create(['kubefaas-s3', '.yaml']) do |f|
    f.write(values.empty? ? '' : YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'patchworks', '-f', f.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def find(docs, kind, name, namespace = nil)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name && (namespace.nil? || d.dig('metadata', 'namespace') == namespace) }
end
def builder_env(docs)
  find(docs, 'Deployment', 'patchworks-kubefaas-builder').dig('spec', 'template', 'spec', 'containers')
    .find { |c| c['name'] == 'kubefaas-builder' }['env'].to_h { |e| [e['name'], e['value'] || e['valueFrom']] }
end
def generator_script(docs)
  find(docs, 'Job', 'patchworks-credential-generator').dig('spec', 'template', 'spec', 'containers', 0, 'command', 2)
end
# The value the generator's script gives a variable, nil when absent.
def script_value(script, name)
  line = script.lines.find { |l| l.strip.start_with?("#{name}=") }
  line && YAML.safe_load(line.strip.split('=', 2).last)
end
def ref(name, key)
  {'secretKeyRef' => {'name' => name, 'key' => key}}
end

# The installer's KubeFaaS: bundled S3 with a generated login, bundled registry.
KUBEFAAS = {'kubefaas' => {'enabled' => true, 'registry' => {'bundled' => {'enabled' => true}}}}.freeze

generated = render(KUBEFAAS)
copy = find(generated, 'Secret', 'patchworks-kubefaas-s3', 'kubefaas')
assert(copy, 'S3 login copy missing from the KubeFaaS namespace')
access_key = Base64.decode64(copy.dig('data', 'access-key'))
secret_key = Base64.decode64(copy.dig('data', 'secret-key'))
check('minioadmin', access_key, 'copied access key (s3.auth.rootUser)')
assert(secret_key.match?(/\A[A-Za-z0-9]{32}\z/), 'generated secret key is not 32 alphanumerics')
check(ref('patchworks-kubefaas-s3', 'access-key'), builder_env(generated)['AWS_ACCESS_KEY'], 'builder AWS_ACCESS_KEY')
check(ref('patchworks-kubefaas-s3', 'secret-key'), builder_env(generated)['AWS_SECRET_KEY'], 'builder AWS_SECRET_KEY')
script = generator_script(generated)
check(access_key, script_value(script, 's3_root_user'), 'generator S3 user')
check(secret_key, script_value(script, 's3_root_password'), 'generator S3 password, the same as the copy')
assert(script.include?('ensure_secret "patchworks" "patchworks-s3-auth"'), 'generator no longer writes patchworks-s3-auth')
assert(find(generated, 'Deployment', 'patchworks-kubefaas-builder').dig('spec', 'template', 'metadata', 'annotations', 'checksum/s3-login'), 'builder does not roll with the S3 login')

# Every Secret a KubeFaaS pod reads is in the KubeFaaS namespace.
kubefaas_secrets = generated.select { |d| d['kind'] == 'Secret' && d.dig('metadata', 'namespace') == 'kubefaas' }.map { |d| d.dig('metadata', 'name') }
generated.select { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'namespace') == 'kubefaas' }.each do |deployment|
  spec = deployment.dig('spec', 'template', 'spec')
  containers = spec.fetch('containers', []) + spec.fetch('initContainers', [])
  referenced = containers.flat_map do |c|
    c.fetch('env', []).map { |e| e.dig('valueFrom', 'secretKeyRef', 'name') } +
      c.fetch('envFrom', []).map { |e| e.dig('secretRef', 'name') }
  end + spec.fetch('volumes', []).map { |v| v.dig('secret', 'secretName') }
  referenced.compact.uniq.each do |name|
    assert(kubefaas_secrets.include?(name), "#{deployment.dig('metadata', 'name')} reads Secret #{name}, which is not rendered in the KubeFaaS namespace")
  end
end

# An inline login: plain values, no copy, and the generator as before.
inline = render(deep_merge(KUBEFAAS, 's3' => {'auth' => {'rootUser' => 'minio', 'rootPassword' => 'minio-secret'}}))
assert(!find(inline, 'Secret', 'patchworks-kubefaas-s3'), 'copy rendered for an inline login')
check('minio', builder_env(inline)['AWS_ACCESS_KEY'], 'inline AWS_ACCESS_KEY')
check('minio-secret', builder_env(inline)['AWS_SECRET_KEY'], 'inline AWS_SECRET_KEY')

# The user's own Secret, which lookup cannot read here: named as before, so it
# must exist in the KubeFaaS namespace too.
existing = render(deep_merge(KUBEFAAS, 's3' => {'auth' => {'existingSecret' => {'name' => 'my-s3'}}}))
assert(!find(existing, 'Secret', 'patchworks-kubefaas-s3'), 'copy rendered for an unread existingSecret')
check(ref('my-s3', 'root-user'), builder_env(existing)['AWS_ACCESS_KEY'], 'existingSecret AWS_ACCESS_KEY')

# No cache bucket, or KubeFaaS off: no copy, and the generator keeps its own
# random login, never one written into its script.
no_cache = render(deep_merge(KUBEFAAS, 'kubefaas' => {'builder' => {'cacheBucket' => ''}}))
assert(!find(no_cache, 'Secret', 'patchworks-kubefaas-s3'), 'copy rendered without a cache bucket')
assert(!builder_env(no_cache).key?('AWS_ACCESS_KEY'), 'AWS_ACCESS_KEY without a cache bucket')
[no_cache, render({})].each do |docs|
  check('', script_value(generator_script(docs), 's3_root_password'), 'generator S3 password without the copy')
end

puts 'kubefaas_s3_credentials_test.rb: ok'
