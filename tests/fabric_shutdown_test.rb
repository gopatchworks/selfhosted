# Run: ruby tests/fabric_shutdown_test.rb (Helm only; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'

CHART = File.expand_path('../charts/patchworks-app', __dir__)

def assert(value, message)
  raise message unless value
end

def render(values = {}, error: nil)
  Tempfile.create(['fabric-shutdown', '.yaml']) do |file|
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

def pod(documents)
  deployment = fabric(documents, 'Deployment')
  assert(deployment, 'Missing Fabric Deployment')
  deployment.dig('spec', 'template', 'spec')
end

def hooks(pod)
  pod['containers'].to_h { |container| [container['name'], container.dig('lifecycle', 'preStop', 'exec', 'command')] }
end

def check_shutdown(pod, delay, grace)
  assert(pod['terminationGracePeriodSeconds'] == grace, "Wrong Fabric termination allowance: #{pod['terminationGracePeriodSeconds'].inspect}")
  expected = delay.zero? ? nil : ['/bin/sh', '-c', "sleep #{delay}"]
  assert(hooks(pod) == { 'fabric' => expected, 'fabric-nginx' => expected }, "Nginx and PHP-FPM must share the preStop delay: #{hooks(pod).inspect}")
end

def check_budget(documents, max_unavailable)
  budget = fabric(documents, 'PodDisruptionBudget')
  assert(budget, 'Missing Fabric PodDisruptionBudget')
  assert(budget.dig('spec', 'maxUnavailable') == max_unavailable, "Wrong maxUnavailable: #{budget.dig('spec', 'maxUnavailable').inspect}")
  selector = fabric(documents, 'Deployment').dig('spec', 'selector', 'matchLabels')
  assert(budget.dig('spec', 'selector', 'matchLabels') == selector, 'PodDisruptionBudget does not select the Fabric pods')
end

baseline = render
check_shutdown(pod(baseline), 20, 90)
check_budget(baseline, 1)

custom = render('fabric' => {
  'preStopSleepSeconds' => 0, 'terminationGracePeriodSeconds' => 75,
  'fpm' => { 'drainTimeoutSeconds' => 70 }, 'podDisruptionBudget' => { 'maxUnavailable' => '25%' }
})
check_shutdown(pod(custom), 0, 75)
check_budget(custom, '25%')

no_budget = render('fabric' => { 'podDisruptionBudget' => { 'enabled' => false } })
assert(fabric(no_budget, 'PodDisruptionBudget').nil?, 'Disabled Fabric PodDisruptionBudget rendered')

# FrankenPHP has no Nginx sidecar or PHP-FPM drain, so it keeps Kubernetes defaults
# and skips their validation, but still limits voluntary evictions.
franken = render('runtime' => { 'frankenphp' => { 'enabled' => true } },
                 'fabric' => { 'terminationGracePeriodSeconds' => 10 })
spec = pod(franken)
assert(!spec.key?('terminationGracePeriodSeconds') && hooks(spec).values.none?, 'PHP-FPM shutdown settings leaked into FrankenPHP')
check_budget(franken, 1)
check_shutdown(pod(render('runtime' => { 'frankenphp' => { 'enabled' => true } },
                          'fabric' => { 'frankenphp' => { 'enabled' => false } })), 20, 90)

disabled = render('fabric' => { 'enabled' => false })
assert(fabric(disabled, 'Deployment').nil? && fabric(disabled, 'PodDisruptionBudget').nil?, 'Disabled Fabric resources rendered')

[-1, 1.5, 'invalid'].each do |seconds|
  render({ 'fabric' => { 'preStopSleepSeconds' => seconds } }, error: 'non-negative integer seconds')
  render({ 'fabric' => { 'fpm' => { 'drainTimeoutSeconds' => seconds } } }, error: 'non-negative integer seconds')
end
render({ 'fabric' => { 'terminationGracePeriodSeconds' => 80 } }, error: 'must exceed preStopSleepSeconds + fpm.drainTimeoutSeconds')
render({ 'fabric' => { 'preStopSleepSeconds' => 40 } }, error: 'must exceed preStopSleepSeconds + fpm.drainTimeoutSeconds')
check_shutdown(pod(render('fabric' => { 'terminationGracePeriodSeconds' => 81 })), 20, 81)

puts 'Fabric shutdown rendering, runtime overrides, disruption budget and validation tests passed'
