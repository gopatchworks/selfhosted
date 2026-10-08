# Run: ruby tests/fabric_nginx_test.rb (Helm only; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)

def assert(value, message)
  raise message unless value
end

def render(values = {}, error: nil)
  Tempfile.create(['fabric-nginx', '.yaml']) do |file|
    file.write(JSON.generate(values))
    file.flush
    output, stderr, status = Open3.capture3('helm', 'template', 'test', CHART,
      '--namespace', 'install', '--set', 'credentials.autoGenerate=false',
      '--set', 'pusher.existingSecret.name=fixture-pusher',
      '--set', 'app.existingSecret.name=fixture-app',
      '--set', 'passport.existingSecret.name=fixture-passport', '-f', file.path)
    if error
      assert(!status.success? && stderr.include?(error), "Expected #{error.inspect}, got #{stderr}")
      return
    end
    raise stderr unless status.success?
    YAML.load_stream(output).compact
  end
end

def fabric(documents, kind)
  documents.find { |d| d['kind'] == kind && d.dig('metadata', 'labels', 'app.kubernetes.io/component') == 'fabric' }
end

def nginx_files(documents)
  config_map = fabric(documents, 'ConfigMap')
  assert(config_map, 'Missing Fabric Nginx ConfigMap')
  volume = fabric(documents, 'Deployment').dig('spec', 'template', 'spec', 'volumes').find { |v| v['name'] == 'nginx-config' }
  mounted = volume.dig('configMap', 'items').map { |item| item['path'] }
  assert(mounted == config_map['data'].keys, "Mounted Nginx files #{mounted.inspect} differ from #{config_map['data'].keys.inspect}")
  config_map['data']
end

def annotations(documents)
  fabric(documents, 'Deployment').dig('spec', 'template', 'metadata', 'annotations')
end

# Unset leaves the Nginx config, and so the pod checksum, exactly as before.
baseline = render
assert(nginx_files(baseline).keys == ['site.conf'], 'Nginx timeouts rendered without being configured')
assert(!annotations(baseline).key?('checksum/nginx-timeouts'), 'Unset Nginx timeouts changed the pod annotations')
assert(!nginx_files(baseline)['site.conf'].include?('fastcgi_read_timeout'), 'site.conf must stay the shipped file')

configured = render('fabric' => { 'nginx' => { 'fastcgiReadTimeoutSeconds' => 300 } })
assert(nginx_files(configured)['timeouts.conf'].strip == 'fastcgi_read_timeout 300s;', "Wrong timeouts.conf: #{nginx_files(configured)['timeouts.conf'].inspect}")
assert(nginx_files(configured)['site.conf'] == nginx_files(baseline)['site.conf'], 'Configuring a timeout changed site.conf')
assert(annotations(configured)['checksum/nginx'] == annotations(baseline)['checksum/nginx'], 'site.conf checksum changed')
assert(annotations(configured).key?('checksum/nginx-timeouts'), 'Changing the timeout would not roll the Fabric pods')
assert(annotations(configured)['checksum/nginx-timeouts'] != annotations(render('fabric' => { 'nginx' => { 'fastcgiReadTimeoutSeconds' => 120 } }))['checksum/nginx-timeouts'],
       'Different timeouts share a pod checksum')

franken = render('runtime' => { 'frankenphp' => { 'enabled' => true } },
                 'fabric' => { 'nginx' => { 'fastcgiReadTimeoutSeconds' => 300 } })
assert(fabric(franken, 'ConfigMap').nil?, 'FrankenPHP has no Nginx sidecar to configure')

[-1, 1.5, 'invalid', '300s'].each do |seconds|
  render({ 'fabric' => { 'nginx' => { 'fastcgiReadTimeoutSeconds' => seconds } } }, error: 'fabric.nginx.fastcgiReadTimeoutSeconds must be non-negative integer seconds')
end

puts 'Fabric Nginx timeout rendering, pod rollout and validation tests passed'
