# Run: ruby tests/dashboard_broadcasting_test.rb (Helm required; no cluster access).
# The dashboard's websocket must reach the public Soketi ingress on 443/https when
# TLS is terminated outside the chart (ingress.scheme=https), not only when the
# chart holds the TLS secret itself.
require 'json'
require 'yaml'
require 'open3'
require 'tempfile'

def assert(value, message)
  raise message unless value
end

# The broadcasting block of the dashboard's config.js, as {host, port, scheme}.
def broadcasting(values)
  values = {'dashboard' => {'enabled' => true}}.merge(values) { |_key, a, b| a.merge(b) }
  Tempfile.create(['dashboard-broadcasting', '.yaml']) do |file|
    file.write(YAML.dump(values))
    file.flush
    output, error, status = Open3.capture3(
      'helm', 'template', 'test', File.expand_path('../charts/patchworks-app', __dir__),
      '--namespace', 'install', '--show-only', 'templates/dashboard.yaml', '-f', file.path
    )
    raise error unless status.success?

    deployment = YAML.load_stream(output).compact.find { |document| document['kind'] == 'Deployment' }
    assert(deployment, "#{values.inspect}: dashboard Deployment did not render")
    init = deployment.dig('spec', 'template', 'spec', 'initContainers').find { |c| c['name'] == 'dashboard-config' }
    assert(init, "#{values.inspect}: dashboard-config initContainer did not render")
    block = init['args'].join("\n")[/broadcasting: \{(.*?)\}/m, 1]
    assert(block, "#{values.inspect}: config.js has no broadcasting block")
    %w[host port scheme].map do |key|
      line = block[/^\s*#{key}: (.*),$/, 1]
      assert(line, "#{values.inspect}: broadcasting.#{key} missing from #{block.inspect}")
      [key, JSON.parse("[#{line}]").first]
    end.to_h
  end
end

def expect(values, expected, name)
  actual = broadcasting(values)
  assert(actual == expected, "#{name}: expected #{expected.inspect}, got #{actual.inspect}")
end

ingress = {'pusher' => {'enabled' => true, 'ingress' => {'enabled' => true, 'host' => 'wss.example.com'}}}
in_cluster = 'patchworks-soketi.install.svc.cluster.local'

# Plain HTTP ingress stays on 80/http.
expect(ingress, {'host' => 'wss.example.com', 'port' => '80', 'scheme' => 'http'}, 'http ingress')

# TLS terminated outside the chart: no TLS secret, but the public scheme is https.
expect(ingress.merge('ingress' => {'scheme' => 'https'}),
       {'host' => 'wss.example.com', 'port' => '443', 'scheme' => 'https'}, 'ingress.scheme=https')

# The chart terminates TLS itself, whatever ingress.scheme says.
tls = {'pusher' => ingress['pusher'].merge('ingress' => ingress['pusher']['ingress'].merge('tlsSecretName' => 'wss-tls'))}
expect(tls.merge('ingress' => {'scheme' => 'http'}),
       {'host' => 'wss.example.com', 'port' => '443', 'scheme' => 'https'}, 'tlsSecretName')

# Explicit dashboard settings still win over both.
override = {'broadcasting' => {'host' => 'ws.example.net', 'port' => '8443', 'scheme' => 'http'}}
[ingress.merge('ingress' => {'scheme' => 'https'}), tls].each do |values|
  expect(values.merge('dashboard' => override),
         {'host' => 'ws.example.net', 'port' => '8443', 'scheme' => 'http'}, "override over #{values.inspect}")
end

# Without the Soketi ingress, ingress.scheme does not touch the in-cluster or external server.
expect({'pusher' => {'enabled' => true}, 'ingress' => {'scheme' => 'https'}},
       {'host' => in_cluster, 'port' => '6001', 'scheme' => 'http'}, 'in-cluster Soketi')
expect({'pusher' => {'enabled' => true, 'ingress' => {'enabled' => false, 'host' => 'wss.example.com'}},
        'ingress' => {'scheme' => 'https'}},
       {'host' => in_cluster, 'port' => '6001', 'scheme' => 'http'}, 'disabled Soketi ingress')
expect({'pusher' => {'enabled' => false, 'appId' => 'id', 'appKey' => 'key', 'appSecret' => 'secret',
                     'external' => {'host' => 'pusher.example.org', 'port' => 6001, 'scheme' => 'http'}},
        'ingress' => {'scheme' => 'https'}},
       {'host' => 'pusher.example.org', 'port' => '6001', 'scheme' => 'http'}, 'external Pusher')

puts 'Dashboard broadcasting render tests passed'
