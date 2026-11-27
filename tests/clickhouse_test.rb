# Run: ruby tests/clickhouse_test.rb (Helm required; no cluster access).
#
# Platform telemetry: the infra chart runs ClickHouse and creates the database
# and users Core connects as, and the app chart points Core at it, runs
# telemetry:migrate after every install and upgrade, and runs telemetry:ship.
# Core's deployment contract (docs/architecture/platform-telemetry/README.md in
# Core) is what these checks hold the charts to.
require 'yaml'
require 'open3'
require 'tempfile'
INFRA = File.expand_path('../charts/patchworks-infra', __dir__)
APP = File.expand_path('../charts/patchworks-app', __dir__)
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def helm(chart, values)
  Tempfile.create(['clickhouse', '.yaml']) do |f|
    f.write(values.empty? ? '' : YAML.dump(values)); f.flush
    Open3.capture3('helm', 'template', 'test', chart, '--namespace', 'patchworks', '-f', f.path)
  end
end
def render(chart, values = {})
  output, error, status = helm(chart, values)
  raise error unless status.success?
  YAML.load_stream(output).compact
end
def render_error(chart, values)
  _, error, status = helm(chart, values)
  assert(!status.success?, "expected #{values.inspect} to fail")
  error
end
def find(docs, kind, name)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name }
end
def container(doc, name = nil)
  containers = doc.dig('spec', 'template', 'spec', 'containers')
  name ? containers.find { |c| c['name'] == name } : containers.first
end
def env(c)
  c.fetch('env', []).to_h { |e| [e['name'], e['value'] || e['valueFrom']] }
end
def ref(name, key)
  {'secretKeyRef' => {'name' => name, 'key' => key}}
end
# Core's references are optional: a missing Secret stops telemetry, never a Core pod.
def oref(name, key)
  {'secretKeyRef' => {'name' => name, 'key' => key, 'optional' => true}}
end
def pods(docs)
  docs.select { |d| %w[Deployment Job].include?(d['kind']) } +
    docs.select { |d| d['kind'] == 'CronJob' }.map { |d| {'kind' => 'CronJob', 'metadata' => d['metadata'], 'spec' => d.dig('spec', 'jobTemplate', 'spec')} }
end

BUNDLED = {'clickhouse' => {'enabled' => true}, 'telemetry' => {'enabled' => true}}.freeze
AUTH = 'patchworks-clickhouse-auth'

# ── Off by default: nothing renders, and no Core pod is told to record ────────
assert(render(INFRA).none? { |d| d.dig('metadata', 'name').to_s.include?('clickhouse') }, 'infra renders ClickHouse while clickhouse.enabled is false')
app_default = render(APP)
assert(app_default.none? { |d| d.dig('metadata', 'name').to_s.include?('telemetry') }, 'app renders telemetry while telemetry.enabled is false')
assert(!YAML.dump(app_default).include?('TELEMETRY_'), 'app sets TELEMETRY_* while telemetry.enabled is false')

# ── Infra: the server ──────────────────────────────────────────────────────────
infra = render(INFRA, BUNDLED)
server = find(infra, 'Deployment', 'patchworks-clickhouse')
assert(server, 'ClickHouse Deployment missing')
check('Recreate', server.dig('spec', 'strategy', 'type'), 'ClickHouse strategy (one server per data directory)')
server_env = env(container(server, 'clickhouse'))
check('admin', server_env['CLICKHOUSE_USER'], 'admin user')
check(ref(AUTH, 'admin-password'), server_env['CLICKHOUSE_PASSWORD'], 'admin password')
check('1', server_env['CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT'], 'SQL access management, to create the telemetry users')
mounts = container(server, 'clickhouse')['volumeMounts'].to_h { |m| [m['mountPath'], m] }
assert(mounts['/var/lib/clickhouse'], 'data volume not mounted at /var/lib/clickhouse')
assert(mounts['/etc/clickhouse-server/config.d/patchworks.xml'], 'patchworks.xml not mounted into config.d')
check('10Gi', find(infra, 'PersistentVolumeClaim', 'patchworks-clickhouse').dig('spec', 'resources', 'requests', 'storage'), 'PVC size, through patchworks.pvcSpec')
check({'http' => 8123, 'native' => 9000}, find(infra, 'Service', 'patchworks-clickhouse')['spec']['ports'].to_h { |p| [p['name'], p['port']] }, 'Service ports')
config = find(infra, 'ConfigMap', 'patchworks-clickhouse').dig('data', 'patchworks.xml')
assert(config.include?('<console>1</console>') && config.include?('<trace_log remove="1"/>'), 'server config no longer logs to the console only and drops the system logs')
assert(server.dig('spec', 'template', 'metadata', 'annotations', 'checksum/config'), 'ClickHouse does not restart when its config changes')
check(0, render(INFRA, BUNDLED.merge('clickhouse' => {'enabled' => true, 'persistence' => {'existingClaim' => 'mine'}})).count { |d| d['kind'] == 'PersistentVolumeClaim' && d.dig('metadata', 'name') == 'patchworks-clickhouse' }, 'PVCs created with persistence.existingClaim')

# ── Infra: generated logins ────────────────────────────────────────────────────
generator = container(find(infra, 'Job', 'patchworks-credential-generator'))['command'][2]
assert(generator.include?(%(ensure_secret "patchworks" "#{AUTH}")), "generator does not write #{AUTH}")
%w[admin-password migrator-password writer-password reader-password].each do |key|
  assert(generator.include?("--from-literal=#{key}="), "generator does not write #{key}")
end
named = render(INFRA, BUNDLED.merge('clickhouse' => {'enabled' => true, 'auth' => {'existingSecret' => {'name' => 'my-clickhouse'}}}))
assert(!container(find(named, 'Job', 'patchworks-credential-generator'))['command'][2].include?(AUTH), 'generator writes ClickHouse logins although auth.existingSecret is named')
check(ref('my-clickhouse', 'admin-password'), env(container(find(named, 'Deployment', 'patchworks-clickhouse')))['CLICKHOUSE_PASSWORD'], 'admin password from auth.existingSecret')
no_generator = BUNDLED.merge('credentials' => {'autoGenerate' => false}, 'pusher' => {'appId' => 'a', 'appKey' => 'b', 'appSecret' => 'c'})
assert(render_error(INFRA, no_generator).include?('clickhouse.auth.adminPassword'), 'no error without passwords, a Secret or credentials.autoGenerate')

# ── Infra: the setup Job mirrors Core's reference script ──────────────────────
setup = find(infra, 'Job', 'patchworks-clickhouse-setup')
assert(setup, 'ClickHouse setup Job missing')
check('post-install,post-upgrade', setup.dig('metadata', 'annotations', 'helm.sh/hook'), 'setup Job hook')
sql = container(setup)['command'][2]
assert(!sql.include?('OR REPLACE'), 'setup SQL replaces a user or profile, which wipes the migrator grants every view runs with')
# CREATE ... IF NOT EXISTS never changes an existing profile, so its settings must come from ALTER.
assert(sql !~ /CREATE SETTINGS PROFILE[^;]*SETTINGS/, 'a profile setting is only in CREATE, so upgrades never apply it')
[
  "ALTER SETTINGS PROFILE telemetry_writer_profile SETTINGS\n    deduplicate_insert = 'enable' CONST",
  'deduplicate_blocks_in_dependent_materialized_views = 1 CONST',
  "ALTER SETTINGS PROFILE telemetry_reader_profile SETTINGS\n    readonly = 2",
  # 65, not the reference's 60: Grafana's ClickHouse plugin asks for its 60s query timeout plus 5.
  'max_execution_time = 30 MIN 1 MAX 65',
  'max_result_rows = 100000 MIN 1 MAX 1000000',
  'CREATE USER IF NOT EXISTS telemetry_migrator IDENTIFIED BY $(sql_string "$TELEMETRY_MIGRATOR_PASSWORD") DEFAULT DATABASE telemetry;',
  'ALTER USER telemetry_writer IDENTIFIED BY $(sql_string "$TELEMETRY_WRITER_PASSWORD");',
  "ALTER USER telemetry_writer SETTINGS PROFILE 'telemetry_writer_profile';",
  "ALTER USER telemetry_reader SETTINGS PROFILE 'telemetry_reader_profile';",
  'CREATE DATABASE IF NOT EXISTS telemetry;',
  "GRANT SELECT, INSERT, ALTER TABLE, ALTER VIEW, CREATE TABLE, CREATE VIEW, DROP TABLE, DROP VIEW, TRUNCATE\n    ON telemetry.* TO telemetry_migrator;",
  'GRANT INSERT ON telemetry.* TO telemetry_writer;',
  'GRANT SELECT ON telemetry.* TO telemetry_reader;'
].each { |statement| assert(sql.include?(statement), "setup SQL lacks #{statement.inspect}") }
setup_env = env(container(setup))
%w[migrator writer reader].each do |role|
  check(ref(AUTH, "#{role}-password"), setup_env["TELEMETRY_#{role.upcase}_PASSWORD"], "setup Job #{role} password")
end
assert(render_error(INFRA, BUNDLED.merge('clickhouse' => {'enabled' => true, 'users' => {'writer' => 'bad name'}})).include?('must be 1-64 letters'), 'an unsafe user name reaches the setup SQL')

# ── App: Core's configuration ─────────────────────────────────────────────────
app = render(APP, BUNDLED)
data = find(app, 'ConfigMap', 'patchworks-config')['data']
{
  'TELEMETRY_ENABLED' => 'true',
  'TELEMETRY_CLUSTER' => 'selfhosted',
  'TELEMETRY_CLICKHOUSE_HOST' => 'patchworks-clickhouse.patchworks.svc.cluster.local',
  'TELEMETRY_CLICKHOUSE_PORT' => '8123',
  'TELEMETRY_CLICKHOUSE_SCHEME' => 'http',
  'TELEMETRY_CLICKHOUSE_DATABASE' => 'telemetry',
  'TELEMETRY_CLICKHOUSE_WRITER_USERNAME' => 'telemetry_writer',
  'TELEMETRY_CLICKHOUSE_READER_USERNAME' => 'telemetry_reader'
}.each { |key, value| check(value, data[key], "Core config #{key}") }
gateway_env = env(container(find(app, 'Deployment', 'patchworks-gateway')))
check(oref(AUTH, 'writer-password'), gateway_env['TELEMETRY_CLICKHOUSE_WRITER_PASSWORD'], 'gateway writer password')
check(oref(AUTH, 'reader-password'), gateway_env['TELEMETRY_CLICKHOUSE_READER_PASSWORD'], 'gateway reader password')

# Only telemetry:migrate connects as the migrator, which owns every view.
pods(app).each do |pod|
  name = pod.dig('metadata', 'name')
  next if name == 'patchworks-telemetry-setup'
  pod.dig('spec', 'template', 'spec', 'containers').each do |c|
    assert(!env(c).key?('TELEMETRY_CLICKHOUSE_MIGRATOR_PASSWORD'), "#{name} gets the migrator password")
  end
end
assert(!data.key?('TELEMETRY_CLICKHOUSE_MIGRATOR_PASSWORD'), 'migrator password in the Core ConfigMap')

# ── App: telemetry:migrate never fails the release ─────────────────────────────
job = find(app, 'Job', 'patchworks-telemetry-setup')
assert(job, 'telemetry setup Job missing')
check('post-install,post-upgrade', job.dig('metadata', 'annotations', 'helm.sh/hook'), 'telemetry setup Job hook')
script = container(job)['command'][2]
assert(script.include?('artisan telemetry:migrate') && script.strip.end_with?('exit 0'), 'telemetry:migrate can fail the release')
assert(script.include?('/opt/patchworks-telemetry/enable-monitoring.php'), 'setup Job does not turn on the Monitoring page')
job_env = env(container(job))
check('telemetry_migrator', job_env['TELEMETRY_CLICKHOUSE_MIGRATOR_USERNAME'], 'migrate Job migrator user')
%w[migrator writer reader].each { |role| check(oref(AUTH, "#{role}-password"), job_env["TELEMETRY_CLICKHOUSE_#{role.upcase}_PASSWORD"], "migrate Job #{role} password") }
assert(find(app, 'ConfigMap', 'patchworks-telemetry-setup').dig('data', 'enable-monitoring.php').include?("'platform-telemetry'"), 'Monitoring page script missing')
manual = render(APP, BUNDLED.merge('telemetry' => {'enabled' => true, 'monitoringPage' => {'enabled' => false}}))
assert(!find(manual, 'ConfigMap', 'patchworks-telemetry-setup') && !container(find(manual, 'Job', 'patchworks-telemetry-setup'))['command'][2].include?('enable-monitoring'), 'monitoringPage.enabled false still turns on the feature')

# ── App: the shipper ──────────────────────────────────────────────────────────
shipper = find(app, 'Deployment', 'patchworks-telemetry-shipper')
assert(shipper, 'telemetry shipper missing')
shipper_container = container(shipper)
check(['artisan', 'telemetry:ship', '--max-time=3600', '--memory=256'], shipper_container['args'], 'shipper command')
assert(shipper.dig('spec', 'template', 'spec', 'terminationGracePeriodSeconds') >= 30, 'shipper stop grace period is under 30s')
check(oref(AUTH, 'writer-password'), env(shipper_container)['TELEMETRY_CLICKHOUSE_WRITER_PASSWORD'], 'shipper writer password')
assert(render_error(APP, BUNDLED.merge('telemetry' => {'enabled' => true, 'shipper' => {'terminationGracePeriodSeconds' => 10}})).include?('at least 30'), 'a grace period under 30s is accepted')
assert(render_error(APP, BUNDLED.merge('telemetry' => {'enabled' => true, 'shipper' => {'replicas' => 0}})).include?('replicas must be at least 1'), 'telemetry without a shipper is accepted')

# ── App: validation and external servers ──────────────────────────────────────
assert(render_error(APP, {'telemetry' => {'enabled' => true}}).include?('needs a ClickHouse'), 'telemetry without a ClickHouse is accepted')
assert(render_error(APP, BUNDLED.merge('telemetry' => {'enabled' => true, 'cluster' => ''})).include?('telemetry.cluster must be set'), 'an empty telemetry.cluster is accepted')
external = {'telemetry' => {'enabled' => true}, 'clickhouse' => {'external' => {'host' => 'clickhouse.example.com', 'port' => 8443, 'scheme' => 'https'}}}
assert(render_error(APP, external).include?('ClickHouse migrator password'), 'an external server without passwords is accepted')
external_secret = render(APP, external.merge('clickhouse' => external['clickhouse'].merge('external' => external.dig('clickhouse', 'external').merge('existingSecret' => {'name' => 'telemetry-logins'}))))
external_data = find(external_secret, 'ConfigMap', 'patchworks-config')['data']
check(%w[clickhouse.example.com 8443 https], external_data.values_at('TELEMETRY_CLICKHOUSE_HOST', 'TELEMETRY_CLICKHOUSE_PORT', 'TELEMETRY_CLICKHOUSE_SCHEME'), 'external server address')
check(oref('telemetry-logins', 'reader-password'), env(container(find(external_secret, 'Deployment', 'patchworks-gateway')))['TELEMETRY_CLICKHOUSE_READER_PASSWORD'], 'reader password from external.existingSecret')
inline = render(APP, external.merge('clickhouse' => external['clickhouse'].merge('external' => external.dig('clickhouse', 'external').merge('migratorPassword' => 'm', 'writerPassword' => 'w', 'readerPassword' => 'r'))))
inline_secret = find(inline, 'Secret', 'patchworks-secret')['stringData']
check(%w[w r], inline_secret.values_at('TELEMETRY_CLICKHOUSE_WRITER_PASSWORD', 'TELEMETRY_CLICKHOUSE_READER_PASSWORD'), 'inline passwords in the app Secret')
assert(!inline_secret.key?('TELEMETRY_CLICKHOUSE_MIGRATOR_PASSWORD'), 'inline migrator password in the app Secret')
check('m', env(container(find(inline, 'Job', 'patchworks-telemetry-setup')))['TELEMETRY_CLICKHOUSE_MIGRATOR_PASSWORD'], 'inline migrator password for telemetry:migrate')

puts 'clickhouse_test: ok'
