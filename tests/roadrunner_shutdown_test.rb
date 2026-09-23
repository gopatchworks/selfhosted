# Run: ruby tests/roadrunner_shutdown_test.rb (Helm and PHP with posix/pcntl; no cluster).
require 'yaml'
require 'json'
require 'open3'
require 'tempfile'
require 'tmpdir'
require 'timeout'

CHART = File.expand_path('../charts/patchworks-app', __dir__)
PHP = ENV.fetch('PHP_BINARY', 'php')

def assert(value, message)
  raise message unless value
end

def render(values = {}, error: nil)
  Tempfile.create(['roadrunner-shutdown', '.yaml']) do |file|
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

def pod(documents, component)
  document = documents.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'labels', 'app.kubernetes.io/component') == component }
  assert(document, "Missing #{component} Deployment")
  document.dig('spec', 'template', 'spec')
end

def hook(pod)
  pod.dig('containers', 0, 'lifecycle', 'preStop', 'exec', 'command')
end

def check_hook(pod, delay, timeout, grace, state_file = '/var/www/html/storage/logs/octane-server-state.json')
  command = hook(pod)
  assert(command[0..1] == ['/usr/local/bin/php', '-r'], 'Hook must invoke PHP directly')
  assert(command[-3..-1] == [delay.to_s, timeout.to_s, state_file], 'Shutdown values did not reach the hook')
  assert(pod['terminationGracePeriodSeconds'] == grace, 'Wrong pod termination allowance')
  command
end

baseline = render
command = check_hook(pod(baseline, 'gateway'), 20, 160, 200)
check_hook(pod(baseline, 'start'), 20, 160, 200)
baseline.select { |d| d['kind'] == 'Deployment' && !%w[gateway start].include?(d.dig('metadata', 'labels', 'app.kubernetes.io/component')) }.each do |document|
  assert(!document.to_json.include?('RoadRunner drain:'), 'Drain hook leaked into another workload')
end

custom_state = '/tmp/octane state "quoted".json'
custom = render('web' => {
  'preStopSleepSeconds' => 5, 'terminationGracePeriodSeconds' => 240,
  'gateway' => { 'preStopSleepSeconds' => 0, 'terminationGracePeriodSeconds' => 250,
                 'roadrunner' => { 'drainTimeoutSeconds' => 220, 'stateFile' => custom_state } }
})
check_hook(pod(custom, 'gateway'), 0, 220, 250, custom_state)
check_hook(pod(custom, 'start'), 5, 160, 240)

franken = render('runtime' => { 'frankenphp' => { 'enabled' => true } })
%w[gateway start].each do |component|
  spec = pod(franken, component)
  assert(hook(spec).nil? && !spec.key?('terminationGracePeriodSeconds'), 'RoadRunner settings leaked into FrankenPHP')
end
mixed = render('runtime' => { 'frankenphp' => { 'enabled' => true } },
               'web' => { 'start' => { 'frankenphp' => { 'enabled' => false } } })
assert(hook(pod(mixed, 'gateway')).nil?, 'Gateway runtime override was ignored')
check_hook(pod(mixed, 'start'), 20, 160, 200)
disabled = render('web' => { 'gateway' => { 'enabled' => false }, 'start' => { 'enabled' => false } })
assert(disabled.none? { |d| %w[gateway start].include?(d.dig('metadata', 'labels', 'app.kubernetes.io/component')) }, 'Disabled web resources rendered')

[-1, 1.5, 'invalid'].each do |delay|
  render({ 'web' => { 'preStopSleepSeconds' => delay } }, error: 'non-negative integer seconds')
end
render({ 'web' => { 'terminationGracePeriodSeconds' => 180 } }, error: 'must exceed')
render({ 'web' => { 'roadrunner' => { 'drainTimeoutSeconds' => 0 } } }, error: 'must exceed')
render({ 'web' => { 'roadrunner' => { 'stateFile' => 'relative.json' } } }, error: 'absolute path')

def run_hook(command, state_file, delay: 0, timeout: 2)
  Timeout.timeout(delay + timeout + 5) do
    Open3.capture3(PHP, *command[1..2], delay.to_s, timeout.to_s, state_file)
  end
end

def fake_server(directory, ignore_term: false)
  source = <<~'PHP'
    pcntl_async_signals(true);
    pcntl_signal(SIGTERM, function () use ($argv) {
        file_put_contents($argv[1], 'signalled');
        if ($argv[3] === 'ignore') {
            return;
        }
        usleep(400000);
        file_put_contents($argv[2], 'request complete');
        exit(0);
    });
    echo "ready\n";
    while (true) {
        usleep(100000);
    }
  PHP
  signal_file = File.join(directory, 'signal')
  completed_file = File.join(directory, 'completed')
  Open3.popen3(PHP, '-r', source, signal_file, completed_file, ignore_term ? 'ignore' : 'drain') do |stdin, stdout, stderr, waiter|
    stdin.close
    assert(Timeout.timeout(5) { stdout.gets } == "ready\n", 'Fake server did not start')
    begin
      yield waiter.pid, signal_file, completed_file, waiter
    ensure
      if waiter.alive?
        Process.kill('KILL', waiter.pid) rescue Errno::ESRCH
      end
      waiter.join
    end
  end
end

Dir.mktmpdir('roadrunner-drain') do |directory|
  state_file = File.join(directory, 'octane state "quoted".json')
  _, error, status = run_hook(command, state_file)
  assert(status.success?, "Missing state file should be a no-op: #{error}")

  [0, 1, -1, '123', nil].each do |pid|
    File.write(state_file, JSON.generate('masterProcessId' => pid))
    _, error, status = run_hook(command, state_file)
    assert(!status.success? && error.include?('invalid master process ID'), "Unsafe PID accepted: #{pid.inspect}")
  end
  File.write(state_file, 'invalid json')
  _, error, status = run_hook(command, state_file)
  assert(!status.success? && error.include?('invalid master process ID'), 'Malformed state was accepted')

  exited_pid = Process.spawn(PHP, '-r', 'exit(0);')
  Process.wait(exited_pid)
  File.write(state_file, JSON.generate('masterProcessId' => exited_pid))
  _, error, status = run_hook(command, state_file)
  assert(status.success?, "Already-exited process should be a no-op: #{error}")

  fake_server(directory) do |pid, signal_file, completed_file, waiter|
    File.write(state_file, JSON.generate('masterProcessId' => pid))
    began = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    runner = Thread.new { run_hook(command, state_file, delay: 1) }
    sleep 0.2
    assert(!File.exist?(signal_file), 'RoadRunner was signalled before the routing delay')
    _, error, status = runner.value
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - began
    assert(status.success?, "Graceful drain failed: #{error}")
    assert(File.read(completed_file) == 'request complete', 'Hook returned before the in-flight request completed')
    assert(elapsed >= 1.4 && elapsed < 4, "Hook did not wait for request completion or waited its full budget: #{elapsed}")
    assert(waiter.value.success?, 'Server did not exit cleanly')
  end

  fake_server(directory, ignore_term: true) do |pid, signal_file, completed_file, waiter|
    File.write(state_file, JSON.generate('masterProcessId' => pid))
    began = Process.clock_gettime(Process::CLOCK_MONOTONIC)
    _, error, status = run_hook(command, state_file, timeout: 1)
    elapsed = Process.clock_gettime(Process::CLOCK_MONOTONIC) - began
    assert(!status.success? && error.include?('timed out'), 'Hung server did not produce a drain timeout')
    assert(elapsed >= 1 && elapsed < 3, "Drain wait was not bounded: #{elapsed}")
    assert(waiter.alive?, 'Hook force-killed the server instead of leaving termination to Kubernetes')
  end
end

puts 'RoadRunner shutdown rendering, runtime overrides, validation, request drain and timeout tests passed'
