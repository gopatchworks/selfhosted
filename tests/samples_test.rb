# Run: ruby tests/samples_test.rb (Helm required; PHP optional; no cluster access).
#
# samples.enabled renders a ConfigMap holding files/samples/*.php verbatim and a
# post-install/post-upgrade hook Job that runs install.php with Core's image as
# the seeded company's admin. It needs mono workers with allCompanies and
# Monocore's optional shapes on, and fails the render otherwise. Off, the chart
# renders as before.
#
# With PHP on the PATH it also lints the sample scripts and checks the layout
# port (files/samples/layout.php) against positions the dashboard's own
# applyDfsLayout gave for the same graphs (tests/fixtures/samples_layout.json).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
SAMPLES = File.join(CHART, 'files', 'samples')
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
SHAPES = {'merge' => 'WORKER_ENABLE_MERGE',
          'dataTransform' => 'WORKER_ENABLE_DATA_TRANSFORM', 'crossReference' => 'WORKER_ENABLE_CROSS_REFERENCE',
          'syncSubprocess' => 'WORKER_ENABLE_SYNC_SUBPROCESS'}.freeze
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render_raw(values)
  Tempfile.create(['samples', '.yaml']) do |f|
    f.write(values.empty? ? "" : YAML.dump(values)); f.flush
    Open3.capture3('helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS, '-f', f.path)
  end
end
def render(values)
  output, error, status = render_raw(values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def fails(values, message)
  _, error, status = render_raw(values)
  assert(!status.success? && error.include?(message), "expected failure #{message.inspect}: #{error}")
end
def find(docs, kind, name)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name }
end
def container(job)
  job.dig('spec', 'template', 'spec', 'containers').first
end
def env(job)
  container(job).fetch('env', []).to_h { |e| [e['name'], e['value']] }
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
TENANT = {'seeds' => {'tenant' => {'companyName' => 'Acme', 'adminEmail' => 'admin@example.com'}}}.freeze
SHAPE_ENV = SHAPES.values.map { |name| {'name' => name, 'value' => 'true'} }.freeze
MONO = deep_merge(TENANT, {'workers' => {'type' => 'mono', 'mono' => {'allCompanies' => {'enabled' => true}, 'extraEnv' => SHAPE_ENV}}}).freeze
ON = deep_merge(MONO, {'samples' => {'enabled' => true}}).freeze

# Off: nothing rendered, and the same output as before the option existed.
off = render(MONO)
assert(!find(off, 'Job', 'patchworks-samples') && !find(off, 'ConfigMap', 'patchworks-samples'), 'samples render while off')
check(off, render(deep_merge(MONO, {'samples' => {'enabled' => false}})), 'explicit samples.enabled=false')

# On: the scripts are copied verbatim, so {{flow.variables.*}} placeholders survive.
docs = render(ON)
config = find(docs, 'ConfigMap', 'patchworks-samples')
assert(config, 'samples ConfigMap missing')
%w[install.php lib.php layout.php flows.php].each do |file|
  check(File.read(File.join(SAMPLES, file)).chomp, config.dig('data', file), "ConfigMap #{file}")
end
assert(config.dig('data', 'flows.php').include?('{{flow.variables.wanted}}'), 'flow variable placeholder lost')

job = find(docs, 'Job', 'patchworks-samples')
assert(job, 'samples Job missing')
annotations = job.dig('metadata', 'annotations')
check('post-install,post-upgrade', annotations['helm.sh/hook'], 'samples hook')
check('before-hook-creation,hook-succeeded', annotations['helm.sh/hook-delete-policy'], 'samples hook delete policy')
check('PostSync', annotations['argocd.argoproj.io/hook'], 'samples Argo CD hook')
check('Never', job.dig('spec', 'template', 'spec', 'restartPolicy'), 'samples restartPolicy')
check(0, job.dig('spec', 'backoffLimit'), 'samples backoffLimit')
c = container(job)
check(['/usr/local/bin/php'], c['command'], 'samples command')
check(['/opt/patchworks-samples/install.php'], c['args'], 'samples args')
check('/var/www/html', c['workingDir'], 'samples workingDir')
assert(c['image'].end_with?('/core:' + YAML.load_file(File.join(CHART, 'values.yaml')).dig('image', 'tag')), "samples image #{c['image']}")
check({'name' => 'samples', 'mountPath' => '/opt/patchworks-samples', 'readOnly' => true}, c['volumeMounts'].first, 'samples mount')
check('patchworks-samples', job.dig('spec', 'template', 'spec', 'volumes').first.dig('configMap', 'name'), 'samples volume')
e = env(job)
check('Acme', e['SAMPLES_COMPANY_NAME'], 'company from seeds.tenant')
check('admin@example.com', e['SAMPLES_USER_EMAIL'], 'user from seeds.tenant')
check('Samples', e['SAMPLES_FOLDER'], 'default folder')
assert(e['SAMPLES_CONNECTOR_URL'].match?(%r{\Ahttp://patchworks-workers\.install\.svc\.cluster\.local:8080/healthz\z}), "connector target #{e['SAMPLES_CONNECTOR_URL']}")
assert(%w[APP_KEY DB_HOST].all? { |name| c['env'].any? { |v| v['name'] == name } }, 'samples Job lacks the app environment')
assert(job.dig('spec', 'template', 'metadata', 'annotations', 'checksum/samples'), 'samples checksum missing')

# Overrides.
overridden = env(find(render(deep_merge(ON, {'samples' => {'companyName' => 'Other', 'userEmail' => 'o@example.com', 'folder' => 'Demo'}})), 'Job', 'patchworks-samples'))
check(['Other', 'o@example.com', 'Demo'], overridden.values_at('SAMPLES_COMPANY_NAME', 'SAMPLES_USER_EMAIL', 'SAMPLES_FOLDER'), 'samples overrides')

# FrankenPHP runs the script through php-cli, like the other PHP commands.
franken = container(find(render(deep_merge(ON, {'runtime' => {'frankenphp' => {'enabled' => true}}})), 'Job', 'patchworks-samples'))
check(['/usr/local/bin/frankenphp'], franken['command'], 'frankenphp samples command')
check(['php-cli', '/opt/patchworks-samples/install.php'], franken['args'], 'frankenphp samples args')

# A chart with workers.mono.enable (gopatchworks/selfhosted#19): null follows
# allCompanies, so the shapes are on unless one is set to false.
KEYED = deep_merge(TENANT, {'samples' => {'enabled' => true}, 'workers' => {'type' => 'mono',
  'mono' => {'allCompanies' => {'enabled' => true}, 'enable' => SHAPES.keys.to_h { |k| [k, nil] }}}}).freeze
assert(find(render(KEYED), 'Job', 'patchworks-samples'), 'samples refused with workers.mono.enable.* null')
assert(find(render(deep_merge(KEYED, {'workers' => {'mono' => {'enable' => SHAPES.keys.to_h { |k| [k, 'true'] }}}})), 'Job', 'patchworks-samples'), 'samples refused with workers.mono.enable.* "true"')
fails(deep_merge(KEYED, {'workers' => {'mono' => {'enable' => {'crossReference' => false}}}}), 'workers.mono.enable.crossReference (WORKER_ENABLE_CROSS_REFERENCE)')
fails(deep_merge(KEYED, {'workers' => {'mono' => {'enable' => {'merge' => 'false'}}}}), 'workers.mono.enable.merge (WORKER_ENABLE_MERGE)')

# Refused without what the samples need.
fails(deep_merge(ON, {'workers' => {'mono' => {'allCompanies' => {'enabled' => false}}}}), 'samples.enabled needs workers.enabled, workers.type mono and workers.mono.allCompanies.enabled')
fails(deep_merge(ON, {'workers' => {'type' => 'standalone', 'mono' => {'inbound' => {'enabled' => false}}}}), 'samples.enabled needs workers.enabled, workers.type mono')
# A chart without workers.mono.enable needs each WORKER_ENABLE_* set to "true".
unless YAML.load_file(File.join(CHART, 'values.yaml')).dig('workers', 'mono').key?('enable')
  SHAPES.each do |key, name|
    fails(deep_merge(ON, {'workers' => {'mono' => {'extraEnv' => SHAPE_ENV.reject { |v| v['name'] == name }}}}), "workers.mono.enable.#{key} (#{name})")
  end
  fails(deep_merge(ON, {'workers' => {'mono' => {'extraEnv' => SHAPE_ENV.map { |v| v.merge('value' => 'false') }}}}), "samples.enabled needs Monocore's optional shapes on")
end
fails(deep_merge(ON, {'seeds' => {'tenant' => {'adminEmail' => ''}}}), 'samples.enabled needs the company and user')

# The scripts parse, and the layout port matches the dashboard on recorded graphs.
php, = Open3.capture2('sh', '-c', 'command -v php')
if php.strip.empty?
  puts 'php not found: skipped the sample script lint and layout checks'
else
  Dir[File.join(SAMPLES, '*.php')].sort.each do |file|
    out, status = Open3.capture2e('php', '-l', file)
    assert(status.success?, "php -l #{file}: #{out}")
  end
  fixture = File.expand_path('fixtures/samples_layout.json', __dir__)
  script = <<~'PHP'
    require $argv[1];
    $graphs = json_decode(file_get_contents($argv[2]), true);
    $out = [];
    foreach ($graphs as $name => $g) {
        $out[$name] = PatchworksSamples\Layout::positions($g['nodes'], $g['edges'], 'TB');
    }
    echo json_encode($out);
  PHP
  out, status = Open3.capture2e('php', '-r', script, File.join(SAMPLES, 'layout.php'), fixture)
  assert(status.success?, "layout port: #{out}")
  actual = JSON.parse(out)
  graphs = JSON.parse(File.read(fixture))
  graphs.each do |name, graph|
    check(graph['expected'], actual[name], "layout of #{name}")
  end
  puts "layout port matches the dashboard on #{graphs.size} graphs (#{graphs.values.sum { |g| g['nodes'].size }} nodes)"
end

puts 'samples render tests passed'
