# Run: ruby tests/lightweight_profile_test.rb (Helm required; no cluster access).
#
# profile=lightweight: the gateway Deployment also serves the start routes
# (APP_DOMAINS=start) and the start Service selects its pods; <fullname>-processor
# consumes every enabled processor queue in two pools and runs each processor's
# schedule in place of the processor Deployments and schedule CronJobs.
# The infra chart reads the same profile for smaller Elasticsearch, RabbitMQ,
# Soketi and KubeFaaS requests. profile=standard renders as before.
require 'yaml'
require 'open3'
require 'tempfile'
CHART = File.expand_path('../charts/patchworks-app', __dir__)
FIXTURE_SECRETS = ['--set', 'credentials.autoGenerate=false', '--set', 'pusher.existingSecret.name=fixture-pusher',
                   '--set', 'app.existingSecret.name=fixture-app', '--set', 'passport.existingSecret.name=fixture-passport'].freeze
def assert(value, message)
  raise message unless value
end
def check(expected, actual, what)
  assert(expected == actual, "#{what}: expected #{expected.inspect}, got #{actual.inspect}")
end
def render_raw(values)
  Tempfile.create(['lightweight', '.yaml']) do |f|
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
def names(docs, kind)
  docs.select { |d| d['kind'] == kind }.map { |d| d.dig('metadata', 'name') }.sort
end
def env(doc)
  doc.dig('spec', 'template', 'spec', 'containers').first.fetch('env', []).to_h { |e| [e['name'], e['value']] }
end
def deep_merge(a, b)
  a.merge(b) { |_, x, y| x.is_a?(Hash) && y.is_a?(Hash) ? deep_merge(x, y) : y }
end
# The supervisord programs of the lightweight worker, as {name => {key => value}}.
def programs(docs)
  conf = find(docs, 'ConfigMap', 'patchworks-processor-supervisord').dig('data', 'supervisord.conf')
  conf.split(/^\[program:/).drop(1).to_h do |block|
    name, body = block.split("]\n", 2)
    [name, body.lines.map(&:strip).reject(&:empty?).to_h { |l| l.split('=', 2) }]
  end
end
LIGHT = {'profile' => 'lightweight'}.freeze

# Standard: unchanged, with the profile named explicitly or defaulted.
standard = render({})
check(standard, render({'profile' => 'standard'}), 'explicit standard profile')
assert(!find(standard, 'Deployment', 'patchworks-processor'), 'lightweight worker renders in standard')
assert(!env(find(standard, 'Deployment', 'patchworks-gateway')).key?('APP_DOMAINS'), 'APP_DOMAINS set in standard')
fails({'profile' => 'tiny'}, 'profile must be standard or lightweight')

# Lightweight: one web and one worker Deployment for Core, no CronJobs.
light = render(LIGHT)
core_deployments = names(light, 'Deployment').reject { |n| %w[patchworks-fabric patchworks-dashboard patchworks-s3-manager patchworks-workers].include?(n) }
check(%w[patchworks-gateway patchworks-processor], core_deployments, 'Core Deployments')
check([], names(light, 'CronJob'), 'CronJobs')
check(names(standard, 'Service'), names(light, 'Service'), 'Services')
assert(names(light, 'ConfigMap').none? { |n| n.start_with?('patchworks-processor-') && n != 'patchworks-processor-supervisord' }, 'processor ConfigMaps remain')

gateway = find(light, 'Deployment', 'patchworks-gateway')
check('gateway', env(gateway)['APP_DOMAIN'], 'web APP_DOMAIN')
check('start', env(gateway)['APP_DOMAINS'], 'web APP_DOMAINS')
check(gateway.dig('spec', 'selector', 'matchLabels'), find(light, 'Service', 'patchworks-start').dig('spec', 'selector'), 'start Service selector')

worker = find(light, 'Deployment', 'patchworks-processor')
assert(!env(worker).key?('APP_DOMAIN') && !env(worker).key?('APP_NAME'), 'worker pod sets APP_DOMAIN or APP_NAME for every program')
progs = programs(light)
check(%w[queue queue-slow schedule-gateway schedule-long-processor schedule-medium-processor schedule-scheduler schedule-short-processor schedule-start], progs.keys.sort, 'supervisord programs')
assert(progs['queue']['command'].include?('--queue=scheduler,start,gateway,short-processor,logging '), "main pool queues: #{progs['queue']['command']}")
check('APP_DOMAIN="gateway",RABBITMQ_QUEUE="gateway"', progs['queue']['environment'], 'main pool domain')
check('2', progs['queue']['numprocs'], 'main pool processes')
assert(progs['queue-slow']['command'].include?('--queue=medium-processor,long-processor '), "slow pool queues: #{progs['queue-slow']['command']}")
check('APP_DOMAIN="medium-processor",RABBITMQ_QUEUE="medium-processor"', progs['queue-slow']['environment'], 'slow pool domain')
check('1', progs['queue-slow']['numprocs'], 'slow pool processes')
check('/bin/sh /etc/patchworks/schedule.sh 300', progs['schedule-scheduler']['command'], 'schedule command')
check('APP_DOMAIN="scheduler",RABBITMQ_QUEUE="scheduler"', progs['schedule-scheduler']['environment'], 'schedule domain')
script = find(light, 'ConfigMap', 'patchworks-processor-supervisord').dig('data', 'schedule.sh')
assert(script.include?('timeout "$deadline" /usr/local/bin/php /var/www/html/artisan schedule:run'), "schedule script: #{script}")

# The pod rolls when the programs change, not only the app config.
checksum = ->(docs) { find(docs, 'Deployment', 'patchworks-processor').dig('spec', 'template', 'metadata', 'annotations', 'checksum/supervisord') }
assert(checksum.call(light) != checksum.call(render(deep_merge(LIGHT, 'lightweight' => {'worker' => {'processes' => 3}}))), 'checksum ignores processes')
assert(checksum.call(light) != checksum.call(render(deep_merge(LIGHT, 'runtime' => {'frankenphp' => {'enabled' => true}}))), 'checksum ignores the schedule script')

# A processor's schedule settings still apply: off, suspended, deadline.
tuned = render(deep_merge(LIGHT, 'processors' => [
  {'name' => 'Scheduler', 'queue' => 'scheduler', 'scheduler' => {'activeDeadlineSeconds' => 60}},
  {'name' => 'Start', 'queue' => 'start', 'scheduler' => {'enabled' => false}},
  {'name' => 'Gateway', 'queue' => 'gateway', 'scheduler' => {'suspend' => true}},
  {'name' => 'Long Processor', 'queue' => 'long-processor', 'enabled' => false},
  {'name' => 'Logging', 'queue' => 'logging', 'extraEnv' => [{'name' => 'FIXTURE', 'value' => 'logging'}]}
]))
tuned_progs = programs(tuned)
check(%w[queue schedule-logging schedule-scheduler], tuned_progs.keys.sort, 'programs with tuned processors')
check('/bin/sh /etc/patchworks/schedule.sh 60', tuned_progs['schedule-scheduler']['command'], 'per-processor deadline')
assert(tuned_progs['queue']['command'].include?('--queue=scheduler,start,gateway,logging '), "queues with a disabled processor: #{tuned_progs['queue']['command']}")
check('logging', env(find(tuned, 'Deployment', 'patchworks-processor'))['FIXTURE'], 'processor extraEnv in the shared pod')

# Schedules off: CronJobs as in standard, worker runs queues only.
cron = render(deep_merge(LIGHT, 'lightweight' => {'worker' => {'schedules' => false}}))
check(names(standard, 'CronJob'), names(cron, 'CronJob'), 'CronJobs with schedules off')
check(%w[queue queue-slow], programs(cron).keys.sort, 'programs with schedules off')

# Web not combined: separate gateway and start Deployments, no APP_DOMAINS.
apart = render(deep_merge(LIGHT, 'lightweight' => {'web' => {'combine' => false}}))
assert(find(apart, 'Deployment', 'patchworks-start'), 'start Deployment missing with combine off')
assert(!env(find(apart, 'Deployment', 'patchworks-gateway')).key?('APP_DOMAINS'), 'APP_DOMAINS with combine off')
check(find(standard, 'Service', 'patchworks-start'), find(apart, 'Service', 'patchworks-start'), 'start Service with combine off')

# Start's own env reaches the combined pod.
start_env = render(deep_merge(LIGHT, 'web' => {'start' => {'extraEnv' => [{'name' => 'CALLBACK_RATE_LIMIT', 'value' => '9'}]}}))
check('9', env(find(start_env, 'Deployment', 'patchworks-gateway'))['CALLBACK_RATE_LIMIT'], 'start extraEnv on the combined pod')

# With mono allCompanies the left-out processor and schedules stay out.
mono = render(deep_merge(LIGHT, 'workers' => {'type' => 'mono', 'mono' => {'allCompanies' => {'enabled' => true}}}))
mono_progs = programs(mono)
check(%w[queue queue-slow schedule-long-processor schedule-medium-processor schedule-scheduler schedule-start], mono_progs.keys.sort, 'programs with allCompanies')
assert(mono_progs['queue']['command'].include?('--queue=scheduler,start,gateway,logging '), "queues with allCompanies: #{mono_progs['queue']['command']}")

# Memory requests: the profile's smaller blocks over web.resources and
# workers.resources, so the Core and Monocore pods fit a small single node.
def resources(docs, name)
  find(docs, 'Deployment', name).dig('spec', 'template', 'spec', 'containers').first['resources']
end
def mib(quantity)
  number, unit = quantity.match(/\A(\d+)(Mi|Gi)\z/).captures
  number.to_i * (unit == 'Gi' ? 1024 : 1)
end
# Every container of every replica.
def requested(docs)
  docs.select { |d| d['kind'] == 'Deployment' }.sum do |d|
    (d.dig('spec', 'replicas') || 1) * d.dig('spec', 'template', 'spec', 'containers').sum { |c| (r = c.dig('resources', 'requests', 'memory')) ? mib(r) : 0 }
  end
end
MONO = {'workers' => {'type' => 'mono', 'companies' => [{'name' => 'molten'}]}}.freeze
STANDARD_REQUEST = {'requests' => {'memory' => '900Mi'}}.freeze
standard_mono = render(MONO)
%w[patchworks-gateway patchworks-start patchworks-workers patchworks-workers-molten].each do |name|
  check(STANDARD_REQUEST, resources(standard_mono, name), "standard #{name} resources")
end
check(STANDARD_REQUEST, resources(standard, 'patchworks-processor-scheduler'), 'standard processor resources')
check(STANDARD_REQUEST, resources(standard, 'patchworks-workers'), 'standard standalone worker resources')

light_mono = render(deep_merge(LIGHT, MONO))
check({'requests' => {'memory' => '512Mi'}}, resources(light_mono, 'patchworks-gateway'), 'lightweight gateway resources')
check({'requests' => {'memory' => '384Mi'}}, resources(light_mono, 'patchworks-processor'), 'lightweight processor resources')
check({'requests' => {'memory' => '128Mi'}}, resources(light_mono, 'patchworks-workers'), 'lightweight Monocore hub resources')
check({'requests' => {'memory' => '128Mi'}}, resources(light_mono, 'patchworks-workers-molten'), 'lightweight Monocore company resources')
hub_only = render(deep_merge(LIGHT, 'workers' => {'type' => 'mono'}))
assert(requested(hub_only) <= 1144, "lightweight app memory requests #{requested(hub_only)}Mi exceed 1.2 GB")
# Standalone workers keep workers.resources: their 15 PHP processes need it.
check(STANDARD_REQUEST, resources(light, 'patchworks-workers'), 'lightweight standalone worker resources')

# The profile's requests win; the shared blocks' cpu and limits stay, which
# Monocore's GOMEMLIMIT derives from.
shared = {'requests' => {'cpu' => '100m', 'memory' => '2Gi'}, 'limits' => {'memory' => '1Gi'}}
layered = render(deep_merge(deep_merge(LIGHT, MONO), 'web' => {'resources' => shared}, 'workers' => {'resources' => shared}))
check({'requests' => {'cpu' => '100m', 'memory' => '512Mi'}, 'limits' => {'memory' => '1Gi'}}, resources(layered, 'patchworks-gateway'), 'gateway over web.resources')
check({'requests' => {'cpu' => '100m', 'memory' => '384Mi'}, 'limits' => {'memory' => '1Gi'}}, resources(layered, 'patchworks-processor'), 'processor over workers.resources')
check({'requests' => {'cpu' => '100m', 'memory' => '128Mi'}, 'limits' => {'memory' => '1Gi'}}, resources(layered, 'patchworks-workers'), 'Monocore over workers.resources')
check('921MiB', env(find(layered, 'Deployment', 'patchworks-workers'))['GOMEMLIMIT'], 'Monocore GOMEMLIMIT from the shared limit')

# web.gateway.resources and a company's resources still win; null blocks fall
# back to the shared ones alone.
own = render(deep_merge(deep_merge(LIGHT, MONO),
  'web' => {'gateway' => {'resources' => {'requests' => {'memory' => '1Gi'}}}},
  'workers' => {'companies' => [{'name' => 'molten', 'resources' => {'requests' => {'memory' => '256Mi'}}}]}))
check({'requests' => {'memory' => '1Gi'}}, resources(own, 'patchworks-gateway'), 'web.gateway.resources over the profile')
check({'requests' => {'memory' => '256Mi'}}, resources(own, 'patchworks-workers-molten'), 'company resources over the profile')
unset = render(deep_merge(deep_merge(LIGHT, MONO), 'lightweight' => {'web' => {'resources' => nil}, 'worker' => {'resources' => nil}, 'mono' => {'resources' => nil}}))
%w[patchworks-gateway patchworks-processor patchworks-workers].each do |name|
  check(STANDARD_REQUEST, resources(unset, name), "#{name} with the profile's block null")
end

# Values without the blocks' parents, such as an older release's under
# helm upgrade --reuse-values, still render: unchanged in standard, the shared
# block alone in lightweight.
check(standard, render({'lightweight' => nil}), 'standard without lightweight values')
check(standard_mono, render(deep_merge(MONO, 'lightweight' => {'mono' => nil})), 'standard mono without lightweight.mono')
older = render(deep_merge(deep_merge(LIGHT, MONO), 'lightweight' => {'mono' => nil}))
check(STANDARD_REQUEST, resources(older, 'patchworks-workers'), 'lightweight Monocore without lightweight.mono')

# Web apart: both web pods take the profile's request.
apart_mono = render(deep_merge(deep_merge(LIGHT, MONO), 'lightweight' => {'web' => {'combine' => false}}))
%w[patchworks-gateway patchworks-start].each do |name|
  check({'requests' => {'memory' => '512Mi'}}, resources(apart_mono, name), "#{name} resources with combine off")
end

# Rollouts: the gateway, start, processor and Monocore replace each pod in
# place (maxSurge 0), so an upgrade needs no memory beside the running pods.
# Standalone workers and the standard profile keep Kubernetes' default.
NO_SURGE = {'type' => 'RollingUpdate', 'rollingUpdate' => {'maxSurge' => 0, 'maxUnavailable' => 1}}.freeze
strategy = ->(docs, name) { find(docs, 'Deployment', name).dig('spec', 'strategy') }
%w[patchworks-gateway patchworks-processor patchworks-workers patchworks-workers-molten].each do |name|
  check(NO_SURGE, strategy.call(light_mono, name), "lightweight #{name} strategy")
end
check(NO_SURGE, strategy.call(apart_mono, 'patchworks-start'), 'lightweight start strategy with combine off')
%w[patchworks-gateway patchworks-start patchworks-workers patchworks-workers-molten].each do |name|
  check(nil, strategy.call(standard_mono, name), "standard #{name} strategy")
end
check(nil, strategy.call(light, 'patchworks-workers'), 'lightweight standalone worker strategy')
surging = render(deep_merge(deep_merge(LIGHT, MONO), 'lightweight' => {'rollingUpdate' => nil}))
%w[patchworks-gateway patchworks-processor patchworks-workers].each do |name|
  check(nil, strategy.call(surging, name), "#{name} strategy with lightweight.rollingUpdate null")
end

# Refusals.
fails(deep_merge(LIGHT, 'processorDeployments' => {'autoscaling' => {'enabled' => true}}), 'cannot autoscale per queue')
fails(deep_merge(LIGHT, 'web' => {'start' => {'namespace' => 'elsewhere'}}), 'lightweight.web.combine needs web.gateway and web.start in one namespace')
render(deep_merge(LIGHT, 'web' => {'start' => {'namespace' => 'elsewhere'}}, 'lightweight' => {'web' => {'combine' => false}}))

# The infra chart reads the same profile: Elasticsearch, RabbitMQ and Soketi
# request less, and Elasticsearch's heap stays at half its request.
INFRA = File.expand_path('../charts/patchworks-infra', __dir__)
def render_infra(values)
  Tempfile.create(['lightweight-infra', '.yaml']) do |f|
    f.write(values.empty? ? "" : YAML.dump(values)); f.flush
    output, error, status = Open3.capture3('helm', 'template', 'test', INFRA, '--namespace', 'install', '-f', f.path)
    raise error unless status.success?
    YAML.load_stream(output).compact
  end
end
java_opts = ->(docs) { env(find(docs, 'Deployment', 'patchworks-elasticsearch'))['ES_JAVA_OPTS'] }
infra_standard = render_infra({})
check(infra_standard, render_infra({'profile' => 'standard'}), 'explicit standard infra profile')
check(infra_standard, render_infra({'lightweight' => nil}), 'standard infra without lightweight values')
check({'requests' => {'memory' => '1Gi'}}, resources(infra_standard, 'patchworks-elasticsearch'), 'standard Elasticsearch resources')
check('-Xms512m -Xmx512m', java_opts.call(infra_standard), 'standard Elasticsearch heap')
check({'requests' => {'cpu' => '100m', 'memory' => '512Mi'}, 'limits' => {'memory' => '1Gi'}}, resources(infra_standard, 'patchworks-rabbitmq'), 'standard RabbitMQ resources')
check({'limits' => {'cpu' => '250m', 'memory' => '256Mi'}}, resources(infra_standard, 'patchworks-soketi'), 'standard Soketi resources')

infra_light = render_infra(LIGHT)
check({'requests' => {'memory' => '512Mi'}}, resources(infra_light, 'patchworks-elasticsearch'), 'lightweight Elasticsearch resources')
check('-Xms256m -Xmx256m', java_opts.call(infra_light), 'lightweight Elasticsearch heap')
check({'requests' => {'cpu' => '100m', 'memory' => '256Mi'}, 'limits' => {'memory' => '1Gi'}}, resources(infra_light, 'patchworks-rabbitmq'), 'lightweight RabbitMQ resources')
check({'requests' => {'memory' => '128Mi'}, 'limits' => {'cpu' => '250m', 'memory' => '256Mi'}}, resources(infra_light, 'patchworks-soketi'), 'lightweight Soketi resources')
assert(requested(infra_light) + requested(hub_only) <= 2048, "lightweight app and infra memory requests #{requested(infra_light) + requested(hub_only)}Mi exceed 2Gi")
fails_infra = ->(values, message) do
  Tempfile.create(['lightweight-infra', '.yaml']) do |f|
    f.write(YAML.dump(values)); f.flush
    _, error, status = Open3.capture3('helm', 'template', 'test', INFRA, '--namespace', 'install', '-f', f.path)
    assert(!status.success? && error.include?(message), "expected infra failure #{message.inspect}: #{error}")
  end
end
fails_infra.call({'profile' => 'tiny'}, 'profile must be standard or lightweight')

# Null blocks fall back to the component's own settings.
infra_unset = render_infra(deep_merge(LIGHT, 'lightweight' => {'elasticsearch' => nil, 'rabbitmq' => nil, 'soketi' => nil}))
%w[patchworks-elasticsearch patchworks-rabbitmq patchworks-soketi].each do |name|
  check(resources(infra_standard, name), resources(infra_unset, name), "#{name} with the profile's block null")
end
check('-Xms512m -Xmx512m', java_opts.call(infra_unset), 'Elasticsearch heap with the profile block null')

# KubeFaaS with the bundled registry: one controller and smaller requests, its
# limits kept (dind's 2Gi for image builds), and 16M per function pod. Logins
# and certificates are fixed so that renders compare.
KUBEFAAS = {'kubefaas' => {
  'enabled' => true,
  'auth' => {'username' => 'fixture', 'password' => 'fixture'},
  'builder' => {'tls' => {'mode' => 'existingSecret', 'existingSecret' => {'serverTls' => 'server', 'clientTls' => 'client'}}},
  'registry' => {'bundled' => {'enabled' => true, 'auth' => {'username' => 'fixture', 'password' => 'fixture'}}}
}}.freeze
KUBEFAAS_DEPLOYMENTS = %w[patchworks-kubefaas-controller patchworks-kubefaas-builder patchworks-kubefaas-redis patchworks-kubefaas-image-registry].freeze
# {container => resources} of a Deployment, and its replicas.
def containers(docs, name)
  find(docs, 'Deployment', name).dig('spec', 'template', 'spec', 'containers').to_h { |c| [c['name'], c['resources']] }
end
def replicas(docs, name)
  find(docs, 'Deployment', name).dig('spec', 'replicas')
end
def function_profile(docs)
  YAML.safe_load(find(docs, 'ConfigMap', 'profile-default').dig('data', 'resources'))
end
kf_standard = render_infra(KUBEFAAS)
check(kf_standard, render_infra(deep_merge(KUBEFAAS, 'profile' => 'standard')), 'explicit standard profile with KubeFaaS')
check(3, replicas(kf_standard, 'patchworks-kubefaas-controller'), 'standard KubeFaaS controller replicas')
check({'kubefaas-controller' => {'requests' => {'cpu' => '100m', 'memory' => '128Mi'}}}, containers(kf_standard, 'patchworks-kubefaas-controller'), 'standard KubeFaaS controller resources')
check({'kubefaas-builder' => {'requests' => {'cpu' => '10m', 'memory' => '128Mi'}},
       'registry-proxy' => {'requests' => {'cpu' => '5m', 'memory' => '16Mi'}},
       'dind' => {'requests' => {'cpu' => '10m', 'memory' => '512Mi'}, 'limits' => {'memory' => '2Gi'}}},
      containers(kf_standard, 'patchworks-kubefaas-builder'), 'standard KubeFaaS builder resources')
check({'redis' => {'requests' => {'cpu' => '100m', 'memory' => '512Mi'}, 'limits' => {'memory' => '768Mi'}}}, containers(kf_standard, 'patchworks-kubefaas-redis'), 'standard KubeFaaS Redis resources')
check({'registry' => {'requests' => {'cpu' => '10m', 'memory' => '64Mi'}}}, containers(kf_standard, 'patchworks-kubefaas-image-registry'), 'standard KubeFaaS registry resources')
check('128M', function_profile(kf_standard).dig('requests', 'memory'), 'standard function request')

kf_light = render_infra(deep_merge(LIGHT, KUBEFAAS))
check(1, replicas(kf_light, 'patchworks-kubefaas-controller'), 'lightweight KubeFaaS controller replicas')
check({'kubefaas-controller' => {'requests' => {'cpu' => '100m', 'memory' => '32Mi'}}}, containers(kf_light, 'patchworks-kubefaas-controller'), 'lightweight KubeFaaS controller resources')
check({'kubefaas-builder' => {'requests' => {'cpu' => '10m', 'memory' => '32Mi'}},
       'registry-proxy' => {'requests' => {'cpu' => '5m', 'memory' => '16Mi'}},
       'dind' => {'requests' => {'cpu' => '10m', 'memory' => '128Mi'}, 'limits' => {'memory' => '2Gi'}}},
      containers(kf_light, 'patchworks-kubefaas-builder'), 'lightweight KubeFaaS builder resources')
check({'redis' => {'requests' => {'cpu' => '100m', 'memory' => '16Mi'}, 'limits' => {'memory' => '768Mi'}}}, containers(kf_light, 'patchworks-kubefaas-redis'), 'lightweight KubeFaaS Redis resources')
check({'registry' => {'requests' => {'cpu' => '10m', 'memory' => '16Mi'}}}, containers(kf_light, 'patchworks-kubefaas-image-registry'), 'lightweight KubeFaaS registry resources')
check({'requests' => {'cpu' => '10m', 'memory' => '16M'}, 'limits' => {'cpu' => '', 'memory' => ''}}, function_profile(kf_light), 'lightweight function profile')
# The kubefaas values themselves are unchanged, so checksum/config is too.
checksum_config = ->(docs) { find(docs, 'Deployment', 'patchworks-kubefaas-controller').dig('spec', 'template', 'metadata', 'annotations', 'checksum/config') }
check(checksum_config.call(kf_standard), checksum_config.call(kf_light), 'controller checksum/config across profiles')
kubefaas_requested = requested(kf_light) - requested(render_infra(deep_merge(LIGHT, KUBEFAAS.merge('kubefaas' => {'enabled' => false}))))
check(240, kubefaas_requested, 'lightweight KubeFaaS memory requests (Mi)')

# On a 4 GB Raspberry Pi 5 the kubelet's system reservation leaves 3021Mi
# allocatable, and k3s (CoreDNS and metrics-server, 140Mi) and Contour (192Mi)
# request 332Mi. Add mono workers, KubeFaaS and one script Core has deployed,
# whose pod requests 35M. The pods that still start beside their old one on an
# upgrade (not Recreate, not maxSurge 0) must then still have room; the largest
# is the KubeFaaS builder.
PI_ALLOCATABLE = 3021
PI_OTHERS = 332
script_pod = 35e6 / 2**20
free = PI_ALLOCATABLE - PI_OTHERS - requested(kf_light) - requested(hub_only) - script_pod
surge = (kf_light + hub_only).select { |d| d['kind'] == 'Deployment' }
  .reject { |d| d.dig('spec', 'strategy', 'type') == 'Recreate' || d.dig('spec', 'strategy', 'rollingUpdate', 'maxSurge') == 0 }
  .to_h { |d| [d.dig('metadata', 'name'), requested([d]) / (d.dig('spec', 'replicas') || 1)] }
  .max_by { |_, mib| mib }
check('patchworks-kubefaas-builder', surge.first, 'largest pod that surges on the Pi')
assert(free >= surge.last, "#{free.round}Mi left on a 4 GB Raspberry Pi 5 with KubeFaaS and a script, less than #{surge.first}'s #{surge.last}Mi surge")

# Null blocks at any level fall back to the kubefaas block's own settings, as
# do values without them (helm upgrade --reuse-values from an older release).
kf_unset = render_infra(deep_merge(deep_merge(LIGHT, KUBEFAAS), 'lightweight' => {'kubefaas' => nil}))
KUBEFAAS_DEPLOYMENTS.each do |name|
  check(containers(kf_standard, name), containers(kf_unset, name), "#{name} with lightweight.kubefaas null")
  check(replicas(kf_standard, name), replicas(kf_unset, name), "#{name} replicas with lightweight.kubefaas null")
end
check('128M', function_profile(kf_unset).dig('requests', 'memory'), 'function request with lightweight.kubefaas null')
kf_partial = render_infra(deep_merge(deep_merge(LIGHT, KUBEFAAS), 'lightweight' => {'kubefaas' => {
  'controller' => {'replicaCount' => nil}, 'builder' => {'dind' => nil}, 'functions' => nil
}}))
check(3, replicas(kf_partial, 'patchworks-kubefaas-controller'), 'controller replicas with replicaCount null')
check(containers(kf_light, 'patchworks-kubefaas-controller'), containers(kf_partial, 'patchworks-kubefaas-controller'), 'controller resources with replicaCount null')
check(containers(kf_standard, 'patchworks-kubefaas-builder')['dind'], containers(kf_partial, 'patchworks-kubefaas-builder')['dind'], 'dind with its block null')
check(containers(kf_light, 'patchworks-kubefaas-builder')['kubefaas-builder'], containers(kf_partial, 'patchworks-kubefaas-builder')['kubefaas-builder'], 'builder with dind null')
check('128M', function_profile(kf_partial).dig('requests', 'memory'), 'function request with its block null')

puts 'lightweight_profile_test.rb: ok'
