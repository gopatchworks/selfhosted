# Run: ruby tests/mono_shutdown_test.rb (Helm only; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)

def assert(value, message)
  raise message unless value
end

def render(mono = {}, workers: {}, error: nil)
  values = { 'workers' => { 'type' => 'mono', 'mono' => mono }.merge(workers) }
  Tempfile.create(['mono-shutdown', '.yaml']) do |file|
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

def worker_pods(documents)
  pods = documents.select do |document|
    document['kind'] == 'Deployment' &&
      document.dig('metadata', 'labels', 'app.kubernetes.io/component').to_s.start_with?('workers')
  end
  assert(!pods.empty?, 'No mono worker Deployment rendered')
  pods.to_h { |document| [document.dig('metadata', 'name'), document.dig('spec', 'template', 'spec')] }
end

def hook(pod)
  pod.dig('containers', 0, 'lifecycle', 'preStop', 'exec', 'command')
end

# The hook is opt-in: the shipped default leaves the container untouched.
baseline = render
worker_pods(baseline).each do |name, pod|
  assert(hook(pod).nil?, "#{name} rendered a preStop hook by default")
end
baseline.select { |d| d['kind'] == 'Deployment' }.each do |document|
  next if document.dig('metadata', 'labels', 'app.kubernetes.io/component').to_s.start_with?('workers')
  command = hook(document.dig('spec', 'template', 'spec')).to_a
  assert(command.first != '/bin/sh', "Mono sleep hook leaked into #{document.dig('metadata', 'name')}")
end

# The hub and every independently deployed company share one container template.
enabled = render({ 'preStopSleepSeconds' => 15 }, workers: { 'companies' => [{ 'name' => 'acme' }] })
pods = worker_pods(enabled)
assert(pods.keys.sort == %w[patchworks-workers patchworks-workers-acme],
       "Unexpected worker Deployments: #{pods.keys}")
pods.each do |name, pod|
  assert(hook(pod) == ['/bin/sh', '-c', 'sleep 15'], "#{name} hook was #{hook(pod).inspect}")
  assert(pod['terminationGracePeriodSeconds'] == 3660, "#{name} lost its termination allowance")
end

# The sleep runs before SIGTERM, so it spends the same grace budget as the drain.
[-1, 1.5, 'invalid'].each do |delay|
  render({ 'preStopSleepSeconds' => delay }, error: 'non-negative integer seconds')
end
render({ 'preStopSleepSeconds' => 15, 'terminationGracePeriodSeconds' => 50 },
       error: 'must exceed preStopSleepSeconds plus the scheduler drain timeout')
render({ 'preStopSleepSeconds' => 15, 'terminationGracePeriodSeconds' => 56 })
render({ 'preStopSleepSeconds' => 40, 'terminationGracePeriodSeconds' => 40,
         'scheduler' => { 'mode' => 'disabled' } },
       error: 'terminationGracePeriodSeconds must exceed preStopSleepSeconds')

puts 'mono shutdown tests passed'
