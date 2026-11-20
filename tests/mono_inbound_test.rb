# Run: ruby tests/mono_inbound_test.rb (Helm required; no cluster access).
#
# The hub Monocore worker serving Core's webhook, callback and start API
# endpoints (workers.mono.inbound), its queued-trigger admission
# (workers.mono.scheduler.admitAllTriggers) and the ingress routes to it.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app'].freeze
def assert(value, message)
  raise message unless value
end
def render(values, failure: nil, passport: 'fixture-passport')
  Tempfile.create(['inbound', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    args = ['helm', 'template', 'test', CHART, '--namespace', 'install', *FIXTURE_SECRETS]
    args += ['--set', "passport.existingSecret.name=#{passport}"] if passport
    output, error, status = Open3.capture3(*args, '-f', f.path)
    if failure
      assert(!status.success? && error.include?(failure), "Expected #{failure.inspect}: #{error}")
      return
    end
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
HOSTS = {'start' => 'start.example.com', 'webhook' => 'webhooks.example.com', 'callback' => 'callback.example.com'}.freeze
# A mono release with a company worker and the start hosts, inbound on unless off.
def mono(extra = {}, inbound: true)
  base = {
    'ingress' => {'enabled' => true, 'hosts' => HOSTS},
    'workers' => {'type' => 'mono', 'companies' => [{'name' => 'shop'}], 'mono' => {'inbound' => {'enabled' => inbound}}}
  }
  deep_merge(base, extra)
end
def workers(documents)
  documents.select { |d| d['kind'] == 'Deployment' && d.dig('spec', 'template', 'spec', 'containers', 0, 'name') == 'workers' }
               .to_h { |d| [d.dig('metadata', 'name'), d.dig('spec', 'template', 'spec', 'containers', 0)] }
end
INBOUND_NAMES = %w[PUBLIC_HTTP_PORT INBOUND_CORE_URL INBOUND_RATE_LIMIT_PREFIX INBOUND_QUEUE_SCHEDULE PASSPORT_PUBLIC_KEY
                   WEBHOOK_DOMAIN CALLBACK_DOMAIN WEBHOOK_RATE_LIMIT CALLBACK_RATE_LIMIT MANUAL_RUNS_API_RATE_LIMIT
                   SCHEDULER_ADMIT_ALL_TRIGGERS].freeze
# The inbound env of a worker container, by name: a value, or a valueFrom hash.
def inbound_env(container)
  (container['env'] || []).select { |e| INBOUND_NAMES.include?(e['name']) }
                          .to_h { |e| [e['name'], e.key?('valueFrom') ? e['valueFrom'] : e['value']] }
end
def hub_env(values)
  inbound_env(workers(render(values))['patchworks-workers'])
end
# The backend of each path of the start, webhook and callback hosts.
def routes(documents)
  documents.select { |d| %w[HTTPProxy Ingress].include?(d['kind']) && d.dig('metadata', 'name') =~ /-(start|webhook|callback)$/ }.to_h do |d|
    paths = if d['kind'] == 'HTTPProxy'
              d.dig('spec', 'routes').map { |r| [r['conditions'][0]['prefix'], r['services'][0]['name'], r['services'][0]['port']] }
            else
              d.dig('spec', 'rules', 0, 'http', 'paths').map { |p| [p['path'], p.dig('backend', 'service', 'name'), p.dig('backend', 'service', 'port', 'name')] }
            end
    [d.dig('metadata', 'name'), paths]
  end
end

# ── Off by default ─────────────────────────────────────────────────────────────
documents = render(mono(inbound: false))
check({'patchworks-workers' => {}, 'patchworks-workers-shop' => {}}, workers(documents).transform_values { |c| inbound_env(c) },
  'without inbound no worker gets the public listener')
check([8080, 8081], workers(documents)['patchworks-workers']['ports'].map { |p| p['containerPort'] }, 'the hub keeps its two ports')
check([], documents.select { |d| d['kind'] == 'Service' && d.dig('metadata', 'name').end_with?('-inbound') }, 'no inbound Service')
core_only = [['/', 'patchworks-start', 80]]
check({'patchworks-start' => core_only, 'patchworks-webhook' => core_only, 'patchworks-callback' => core_only}, routes(documents),
  'every start host path stays on Core')

# ── On: the hub serves, configured as Core's start pods ───────────────────────
documents = render(mono)
containers = workers(documents)
check({
  'PUBLIC_HTTP_PORT' => '8082',
  'INBOUND_CORE_URL' => 'http://patchworks-start.install.svc.cluster.local:80',
  'INBOUND_RATE_LIMIT_PREFIX' => 'laravel_cache_:',
  'INBOUND_QUEUE_SCHEDULE' => 'true',
  'PASSPORT_PUBLIC_KEY' => {'secretKeyRef' => {'name' => 'fixture-passport', 'key' => 'PASSPORT_PUBLIC_KEY'}},
  'WEBHOOK_DOMAIN' => 'webhooks.example.com',
  'CALLBACK_DOMAIN' => 'callback.example.com'
}, inbound_env(containers['patchworks-workers']), 'the hub serves inbound triggers with Core start\'s settings')
check({}, inbound_env(containers['patchworks-workers-shop']), 'a company worker serves none')
check([8080, 8081, 8082], containers['patchworks-workers']['ports'].map { |p| p['containerPort'] }, 'the hub exposes the public port')
check([8080, 8081], containers['patchworks-workers-shop']['ports'].map { |p| p['containerPort'] }, 'a company worker does not')
services = documents.select { |d| d['kind'] == 'Service' && d.dig('metadata', 'name') == 'patchworks-workers-inbound' }
check(1, services.size, 'one inbound Service')
check([{'name' => 'http', 'port' => 80, 'targetPort' => 'public'}], services[0].dig('spec', 'ports'),
  'the inbound Service exposes the public listener alone, as port 80')
check({'app.kubernetes.io/name' => 'workers', 'app.kubernetes.io/instance' => 'test'}, services[0].dig('spec', 'selector'),
  'the inbound Service selects the hub alone')

monocore = 'patchworks-workers-inbound'
check({
  'patchworks-start' => [['/api/v1/flows/', monocore, 80], *core_only],
  'patchworks-webhook' => [['/api/v1/otc/', 'patchworks-start', 80], ['/api/v1/mcp', 'patchworks-start', 80], ['/api/v1/', monocore, 80], *core_only],
  'patchworks-callback' => [['/api/v1/otc/', 'patchworks-start', 80], ['/api/v1/mcp', 'patchworks-start', 80], ['/api/v1/', monocore, 80], *core_only]
}, routes(documents), 'Contour sends the paths Monocore serves to it, and the rest to Core')
core_only_nginx = [['/', 'patchworks-start', 'http']]
check({
  'patchworks-start' => [['/api/v1/flows', monocore, 'http'], *core_only_nginx],
  'patchworks-webhook' => [['/api/v1/otc', 'patchworks-start', 'http'], ['/api/v1/mcp', 'patchworks-start', 'http'], ['/api/v1', monocore, 'http'], *core_only_nginx],
  'patchworks-callback' => [['/api/v1/otc', 'patchworks-start', 'http'], ['/api/v1/mcp', 'patchworks-start', 'http'], ['/api/v1', monocore, 'http'], *core_only_nginx]
}, routes(render(mono('ingress' => {'provider' => 'nginx'}))), 'an Ingress routes the same paths')
check({'patchworks-start' => core_only, 'patchworks-webhook' => core_only, 'patchworks-callback' => core_only},
  routes(render(mono('workers' => {'mono' => {'inbound' => {'ingress' => {'enabled' => false}}}}))),
  'inbound.ingress.enabled=false leaves the routes to the operator')
dashboard = render(mono('ingress' => {'hosts' => {'dashboard' => 'app.example.com'}}))
  .select { |d| d['kind'] == 'HTTPProxy' && d.dig('metadata', 'name') == 'patchworks-dashboard' }
check([['/core-start', 'patchworks-start']], dashboard.flat_map { |d| d.dig('spec', 'routes') }
  .map { |r| [r['conditions'][0]['prefix'], r['services'][0]['name']] }.select { |prefix, _| prefix.include?('start') },
  'the dashboard\'s path route to the start service stays on Core')

# ── Rate-limit prefix: Core's config/cache.php ─────────────────────────────────
def start_env(*entries)
  {'web' => {'start' => {'extraEnv' => entries}}}
end
def env(name, value)
  {'name' => name, 'value' => value}
end
{
  'Flow Initialisation Service' => 'flow_initialisation_service_cache_:',
  'start' => 'start_cache_:',
  'My-App@Home  2' => 'my_app_at_home_2_cache_:',
  '--Core__API--' => 'core_api_cache_:'
}.each do |app_name, prefix|
  check(prefix, hub_env(mono(start_env(env('APP_NAME', app_name))))['INBOUND_RATE_LIMIT_PREFIX'],
    "APP_NAME #{app_name.inspect} gives Str::slug's prefix")
end
check('core_cache_:', hub_env(mono(start_env(env('APP_NAME', 'Flow'), env('CACHE_PREFIX', 'core_cache_:'))))['INBOUND_RATE_LIMIT_PREFIX'],
  'CACHE_PREFIX wins over APP_NAME')
check('start_cache_:', hub_env(mono(start_env(env('APP_NAME', 'start')).merge('app' => {'extraEnv' => [env('APP_NAME', 'Core')]})))['INBOUND_RATE_LIMIT_PREFIX'],
  'the start service\'s extraEnv comes after app.extraEnv, and the last entry wins')
check('core_cache_:', hub_env(mono('app' => {'extraEnv' => [env('APP_NAME', 'Core')]}))['INBOUND_RATE_LIMIT_PREFIX'],
  'app.extraEnv reaches Core\'s start pods')
check('web_cache_:', hub_env(mono('web' => {'extraEnv' => [env('APP_NAME', 'Web')]}))['INBOUND_RATE_LIMIT_PREFIX'],
  'web.extraEnv applies when the start service sets none')
check('chosen_cache_:', hub_env(mono(start_env(env('APP_NAME', 'start')).merge('workers' => {'mono' => {'inbound' => {'rateLimitPrefix' => 'chosen_cache_:'}}})))['INBOUND_RATE_LIMIT_PREFIX'],
  'an explicit rateLimitPrefix wins')
render(mono(start_env({'name' => 'APP_NAME', 'valueFrom' => {'secretKeyRef' => {'name' => 'core', 'key' => 'name'}}})),
  failure: 'workers.mono.inbound.rateLimitPrefix is required')
render(mono(start_env(env('APP_NAME', 'Café'))), failure: 'is not ASCII')
render(mono(start_env(env('CACHE_PREFIX', ''))), failure: 'must not be empty')

# ── Queue schedule: Core's FLOW_QUEUE_SCHEDULE ─────────────────────────────────
{'false' => 'false', '0' => 'false', '(null)' => 'false', '' => 'false', 'true' => 'true', '(true)' => 'true', 'no' => 'true'}.each do |core, monocore_value|
  check(monocore_value, hub_env(mono(start_env(env('FLOW_QUEUE_SCHEDULE', core))))['INBOUND_QUEUE_SCHEDULE'],
    "Core's FLOW_QUEUE_SCHEDULE #{core.inspect} reads as PHP reads it")
end
check('false', hub_env(mono('workers' => {'mono' => {'inbound' => {'queueSchedule' => false}}}))['INBOUND_QUEUE_SCHEDULE'],
  'a boolean false override applies')
check('true', hub_env(mono(start_env(env('FLOW_QUEUE_SCHEDULE', 'false')).merge('workers' => {'mono' => {'inbound' => {'queueSchedule' => 'true'}}})))['INBOUND_QUEUE_SCHEDULE'],
  'an explicit queueSchedule wins')
render(mono('workers' => {'mono' => {'inbound' => {'queueSchedule' => 'yes'}}}), failure: 'queueSchedule must be')

# ── Core's settings mirrored, the worker's own extraEnv left alone ─────────────
limit_ref = {'secretKeyRef' => {'name' => 'limits', 'key' => 'callback'}}
mirrored = hub_env(mono(start_env(env('WEBHOOK_RATE_LIMIT', '600'), {'name' => 'CALLBACK_RATE_LIMIT', 'valueFrom' => limit_ref},
                                  env('MANUAL_RUNS_API_RATE_LIMIT', '20'), env('WEBHOOK_DOMAIN', 'hooks.example.com'))))
check(['600', limit_ref, '20', 'hooks.example.com', 'callback.example.com'],
  mirrored.values_at('WEBHOOK_RATE_LIMIT', 'CALLBACK_RATE_LIMIT', 'MANUAL_RUNS_API_RATE_LIMIT', 'WEBHOOK_DOMAIN', 'CALLBACK_DOMAIN'),
  'Core start\'s rate limits and domains reach the hub, valueFrom included')
own = workers(render(mono('workers' => {'mono' => {'extraEnv' => [env('INBOUND_RATE_LIMIT_PREFIX', 'own_cache_:')]}})))['patchworks-workers']
check(['own_cache_:'], own['env'].select { |e| e['name'] == 'INBOUND_RATE_LIMIT_PREFIX' }.map { |e| e['value'] },
  'a name in workers.mono.extraEnv is rendered once, with its value')
check('http://core.internal', hub_env(mono('workers' => {'mono' => {'inbound' => {'coreUrl' => 'http://core.internal'}}}))['INBOUND_CORE_URL'],
  'an explicit coreUrl wins')
check('http://patchworks-start.install.svc.cluster.local:8080', hub_env(mono('web' => {'start' => {'service' => {'port' => 8080}}}))['INBOUND_CORE_URL'],
  'the derived coreUrl follows the start Service\'s port')
existing = render(mono('passport' => {'existingSecret' => {'name' => 'keys', 'publicKeyKey' => 'public'}}), passport: nil)
check({'secretKeyRef' => {'name' => 'keys', 'key' => 'public'}},
  inbound_env(workers(existing)['patchworks-workers'])['PASSPORT_PUBLIC_KEY'], 'an existing Passport Secret is read through its publicKeyKey')
generated = render(mono('credentials' => {'autoGenerate' => true}), passport: nil)
check({'secretKeyRef' => {'name' => 'patchworks-passport-keys', 'key' => 'PASSPORT_PUBLIC_KEY'}},
  inbound_env(workers(generated)['patchworks-workers'])['PASSPORT_PUBLIC_KEY'], 'the generated Passport Secret holds PASSPORT_PUBLIC_KEY')

# ── Queued-trigger admission ───────────────────────────────────────────────────
admitting = workers(render(mono({'workers' => {'mono' => {'scheduler' => {'admitAllTriggers' => true}}}}, inbound: false)))
check({'SCHEDULER_ADMIT_ALL_TRIGGERS' => 'true'}, inbound_env(admitting['patchworks-workers']), 'the hub admits every queued trigger')
check({}, inbound_env(admitting['patchworks-workers-shop']), 'a company worker, which never schedules, does not')
check({}, hub_env(mono({'workers' => {'mono' => {'scheduler' => {'admitAllTriggers' => true, 'mode' => 'disabled'}}}}, inbound: false)),
  'nothing is rendered without a scheduler')

# ── Guards ─────────────────────────────────────────────────────────────────────
only_hub = 'only the hub Monocore worker serves inbound triggers'
render({'workers' => {'type' => 'standalone', 'mono' => {'inbound' => {'enabled' => true}}}}, failure: only_hub)
render(mono('workers' => {'hub' => {'enabled' => false}}), failure: only_hub)
render(mono('web' => {'start' => {'enabled' => false}}), failure: 'workers.mono.inbound.coreUrl is required when web.start is disabled')
# Core's start pods in another release: nothing here describes them.
elsewhere = {'web' => {'start' => {'enabled' => false}}, 'app' => {'extraEnv' => [env('APP_NAME', 'Core')]}}
render(mono(deep_merge(elsewhere, 'workers' => {'mono' => {'inbound' => {'coreUrl' => 'http://core.internal'}}})),
  failure: 'workers.mono.inbound.rateLimitPrefix is required when web.start is disabled')
render(mono(deep_merge(elsewhere, 'workers' => {'mono' => {'inbound' => {'coreUrl' => 'http://core.internal', 'rateLimitPrefix' => 'start_cache_:'}}})),
  failure: 'workers.mono.inbound.queueSchedule is required when web.start is disabled')
separate = render(mono(deep_merge(elsewhere, 'workers' => {'mono' => {'inbound' => {'coreUrl' => 'http://core.internal', 'rateLimitPrefix' => 'start_cache_:', 'queueSchedule' => 'true'}}})))
check(['http://core.internal', 'start_cache_:', 'true'],
  inbound_env(workers(separate)['patchworks-workers']).values_at('INBOUND_CORE_URL', 'INBOUND_RATE_LIMIT_PREFIX', 'INBOUND_QUEUE_SCHEDULE'),
  'with Core\'s start pods elsewhere the hub takes the values given')
check({}, routes(separate), 'and the chart renders no start, webhook or callback routes')
render(mono('web' => {'start' => {'namespace' => 'core'}}), failure: 'an Ingress cannot reach a Service in another namespace')
split = render(mono('web' => {'start' => {'namespace' => 'core'}}, 'workers' => {'mono' => {'inbound' => {'ingress' => {'enabled' => false}}}}))
check({'patchworks-start' => core_only, 'patchworks-webhook' => core_only, 'patchworks-callback' => core_only}, routes(split),
  'routing left to the operator works across namespaces')
check('http://patchworks-start.core.svc.cluster.local:80', inbound_env(workers(split)['patchworks-workers'])['INBOUND_CORE_URL'],
  'the derived coreUrl names the start Service\'s namespace')
render(mono('workers' => {'mono' => {'scheduler' => {'mode' => 'disabled'}, 'autoscaling' => {'enabled' => true, 'minReplicas' => 0}}}),
  failure: 'serving inbound triggers cannot scale or pause to zero')

puts 'Mono hub serves inbound triggers'
