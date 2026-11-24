# Run: ruby tests/rabbitmq_node_name_test.rb (Helm required; no cluster access).
#
# RabbitMQ names its node rabbit@<hostname> and keeps its data in
# /var/lib/rabbitmq/mnesia/rabbit@<hostname>. The Deployment must pin the pod
# hostname, or every new pod starts an empty broker, and must use Recreate, or
# two brokers open the same volume and node directory during a rollout.
require 'yaml'
require 'open3'
require 'tempfile'

def assert(value, message)
  raise message unless value
end

def render(values = {})
  Tempfile.create(['rabbitmq-node-name', '.yaml']) do |file|
    file.write(YAML.dump(values))
    file.flush
    # Helm rejects the "--- {}" YAML.dump writes for an empty hash.
    values_args = values.empty? ? [] : ['-f', file.path]
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', File.expand_path('../charts/patchworks-infra', __dir__),
      '--namespace', 'install', *values_args
    )
    raise error unless status.success?

    YAML.load_stream(output).compact
  end
end

def rabbitmq(docs)
  docs.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'labels', 'app.kubernetes.io/component') == 'rabbitmq' }
end

def recreate_hook(docs)
  docs.select { |d| d.dig('metadata', 'labels', 'app.kubernetes.io/component') == 'rabbitmq-recreate' }
end

def hostname(deployment)
  deployment.dig('spec', 'template', 'spec', 'hostname')
end

# Pinned hostname and Recreate, with no rollingUpdate block (the API server
# rejects one beside type: Recreate).
docs = render
deployment = rabbitmq(docs)
assert(deployment, 'RabbitMQ Deployment did not render')
assert(deployment.dig('spec', 'strategy') == {'type' => 'Recreate'},
       "strategy: expected {type: Recreate}, got #{deployment.dig('spec', 'strategy').inspect}")
assert(hostname(deployment) == 'patchworks-rabbitmq', "hostname: got #{hostname(deployment).inspect}")

# The node name comes from the hostname. RABBITMQ_NODENAME would need a host
# part that resolves inside the pod, so the chart doesn't set it.
env = deployment.dig('spec', 'template', 'spec', 'containers', 0, 'env').map { |e| e['name'] }
assert(!env.include?('RABBITMQ_NODENAME'), "RABBITMQ_NODENAME is set: #{env.inspect}")

# Recreate covers rollouts only: a deleted or evicted pod's replacement starts
# at once. The container takes a lock on the volume before it runs the image's
# entrypoint, so on the same node the new broker waits for the old one.
container = deployment.dig('spec', 'template', 'spec', 'containers', 0)
mount = container['volumeMounts'].find { |m| m['name'] == 'data' }
assert(mount['mountPath'] == '/var/lib/rabbitmq', "data mount: #{mount.inspect}")
assert(container['command'][0, 2] == %w[sh -c] && container['args'].nil?, "command: #{container['command'].inspect}")
lines = container['command'][2].lines.map(&:strip)
assert(lines.first == 'exec 9>>/var/lib/rabbitmq/.broker.lock', "lock: #{lines.first.inspect}")
assert(lines.include?('flock 9 || echo "Could not lock /var/lib/rabbitmq/.broker.lock; starting anyway."'), "flock: #{lines.inspect}")
assert(lines.last == 'exec docker-entrypoint.sh rabbitmq-server', "entrypoint: #{lines.last.inspect}")

# A change to the pod template rolls the pod but keeps the hostname.
other = rabbitmq(render('rabbitmq' => {'resources' => {'requests' => {'memory' => '256Mi'}}}))
assert(other.dig('spec', 'template', 'spec', 'containers', 0, 'resources') !=
       deployment.dig('spec', 'template', 'spec', 'containers', 0, 'resources'), 'resources did not change the pod template')
assert(hostname(other) == hostname(deployment), "hostname changed with the pod template: #{hostname(other).inspect}")

# It follows the fullname, like the Service.
renamed = render('fullnameOverride' => 'acme')
assert(hostname(rabbitmq(renamed)) == 'acme-rabbitmq', "fullnameOverride: got #{hostname(rabbitmq(renamed)).inspect}")
service = renamed.find { |d| d['kind'] == 'Service' && d.dig('metadata', 'labels', 'app.kubernetes.io/component') == 'rabbitmq' &&
                             d.dig('spec', 'ports', 0, 'name') == 'amqp' }
assert(service.dig('metadata', 'name') == 'acme-rabbitmq', "Service name: got #{service.dig('metadata', 'name').inspect}")

# rabbitmq.hostname keeps an existing broker's node name across the upgrade.
kept = rabbitmq(render('rabbitmq' => {'hostname' => 'patchworks-rabbitmq-69c7b9bb74-hfzqj'}))
assert(hostname(kept) == 'patchworks-rabbitmq-69c7b9bb74-hfzqj', "rabbitmq.hostname: got #{hostname(kept).inspect}")

# The pre-upgrade hook switches a live Deployment to Recreate first. Without a
# cluster, lookup finds nothing, so it renders.
hook = recreate_hook(docs)
assert(hook.map { |d| d['kind'] }.sort == %w[Job Role RoleBinding ServiceAccount],
       "recreate hook: rendered #{hook.map { |d| d['kind'] }.inspect}")
hook.each do |d|
  assert(d.dig('metadata', 'annotations', 'helm.sh/hook') == 'pre-upgrade',
         "#{d['kind']}: hook #{d.dig('metadata', 'annotations', 'helm.sh/hook').inspect}")
end
role = hook.find { |d| d['kind'] == 'Role' }
assert(role.dig('metadata', 'namespace') == 'install', "Role namespace: #{role.dig('metadata', 'namespace').inspect}")
assert(role['rules'] == [{'apiGroups' => ['apps'], 'resources' => ['deployments'],
                          'resourceNames' => ['patchworks-rabbitmq'], 'verbs' => %w[get patch]}],
       "Role rules: #{role['rules'].inspect}")
script = hook.find { |d| d['kind'] == 'Job' }.dig('spec', 'template', 'spec', 'containers', 0, 'command', 2)
assert(script.include?('ns="install"') && script.include?('deployment="patchworks-rabbitmq"'), "Job targets: #{script}")
assert(script.include?(%q('{"spec":{"strategy":{"type":"Recreate","rollingUpdate":null}}}')), "Job patch: #{script}")

# A RabbitMQ namespace override moves the Role there; the Job stays in the
# release namespace.
moved = recreate_hook(render('rabbitmq' => {'namespace' => 'queues'}))
moved_role = moved.find { |d| d['kind'] == 'Role' }
assert(moved_role.dig('metadata', 'namespace') == 'queues', "moved Role namespace: #{moved_role.dig('metadata', 'namespace').inspect}")
assert(moved.find { |d| d['kind'] == 'RoleBinding' }.dig('subjects', 0, 'namespace') == 'install', 'RoleBinding subject left the release namespace')
moved_job = moved.find { |d| d['kind'] == 'Job' }
assert(moved_job.dig('metadata', 'namespace') == 'install', "moved Job namespace: #{moved_job.dig('metadata', 'namespace').inspect}")
assert(moved_job.dig('spec', 'template', 'spec', 'containers', 0, 'command', 2).include?('ns="queues"'), 'Job does not patch in the RabbitMQ namespace')

# External RabbitMQ: no Deployment and no hook.
external = render('rabbitmq' => {'enabled' => false, 'external' => {'host' => 'rabbitmq.example'}})
assert(!rabbitmq(external), 'rabbitmq.enabled=false rendered the Deployment')
assert(recreate_hook(external).empty?, 'rabbitmq.enabled=false rendered the recreate hook')

puts 'RabbitMQ node name and Recreate render tests passed'
