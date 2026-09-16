# Run: ruby tests/microservice_queue_test.rb (Helm required; no cluster access).
require 'yaml'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)

def assert(value, message)
  raise message unless value
end

def render(values = {})
  Tempfile.create(['microservice-queue', '.yaml']) do |file|
    file.write(YAML.dump(values))
    file.flush
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', CHART,
      '--namespace', 'install',
      '--set', 'credentials.autoGenerate=false',
      '--set', 'pusher.existingSecret.name=fixture-pusher',
      '--set', 'app.existingSecret.name=fixture-app',
      '--set', 'passport.existingSecret.name=fixture-passport',
      '-f', file.path
    )
    raise error unless status.success?

    YAML.load_stream(output).compact
  end
end

def resource(documents, kind, name)
  documents.find do |document|
    document['kind'] == kind && document.dig('metadata', 'name') == name
  end
end

documents = render('workers' => {'type' => 'microservice'})
translate_config = resource(documents, 'ConfigMap', 'patchworks-workers-translate-supervisord')
assert(translate_config.dig('data', 'supervisord.conf').include?('--queue=map'), 'Translate worker must consume the map queue')
batch_config = resource(documents, 'ConfigMap', 'patchworks-workers-batch-supervisord')
assert(batch_config.dig('data', 'supervisord.conf').include?('--queue=batch'), 'An unset queue must fall back to the service domain')

translate_deployment = resource(documents, 'Deployment', 'patchworks-workers-translate')
translate_env = translate_deployment.dig('spec', 'template', 'spec', 'containers', 0, 'env')
assert(translate_env.find { |item| item['name'] == 'APP_DOMAIN' }['value'] == 'translate', 'Translate APP_DOMAIN must remain translate')

topology = resource(documents, 'ConfigMap', 'patchworks-rabbitmq-topology').dig('data', 'topology.yaml')
assert(topology.include?('name: "map"'), 'Translate queue override did not reach RabbitMQ topology')
assert(!topology.include?('name: "translate"'), 'RabbitMQ topology used the translate domain instead of its queue override')

documents = render(
  'workers' => {
    'type' => 'microservice',
    'microservices' => {
      'assert' => {'name' => 'Assert', 'domain' => 'assert-domain', 'queue' => 'assert-jobs'}
    }
  }
)
assert_config = resource(documents, 'ConfigMap', 'patchworks-workers-assert-supervisord')
assert(assert_config.dig('data', 'supervisord.conf').include?('--queue=assert-jobs'), 'Per-service queue override did not reach supervisord')

assert_deployment = resource(documents, 'Deployment', 'patchworks-workers-assert')
assert_env = assert_deployment.dig('spec', 'template', 'spec', 'containers', 0, 'env')
assert(assert_env.find { |item| item['name'] == 'APP_DOMAIN' }['value'] == 'assert-domain', 'Queue override changed APP_DOMAIN')

topology = resource(documents, 'ConfigMap', 'patchworks-rabbitmq-topology').dig('data', 'topology.yaml')
assert(topology.include?('name: "assert-jobs"'), 'Per-service queue override did not reach RabbitMQ topology')
assert(!topology.include?('name: "assert-domain"'), 'RabbitMQ topology used domain despite queue override')

puts 'Microservice queue overrides and translate map queue passed'
