# Run: ruby tests/kubefaas_dind_mtu_test.rb (Helm required; no cluster access).
#
# The KubeFaaS builder's image builds run on dind's Docker bridge. Docker gives
# it MTU 1500, which is too large on overlay pod networks such as k3s's flannel
# (1450): on a Raspberry Pi 5, downloads from GitHub then stalled and base image
# builds hung or failed. dind therefore starts with --mtu: the pod's eth0 MTU,
# read at startup, or kubefaas.builder.dind.mtu when set.
require 'yaml'
require 'open3'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def check(expected, actual, what)
  raise "#{what}: expected #{expected.inspect}, got #{actual.inspect}" unless expected == actual
end
def dind(*sets)
  args = ['helm', 'template', 'test', CHART, '--namespace', 'patchworks', '--set', 'kubefaas.enabled=true']
  sets.each { |s| args += ['--set', s] }
  output, error, status = Open3.capture3(*args)
  raise error unless status.success?
  builder = YAML.load_stream(output).compact.find { |d| d['kind'] == 'Deployment' && d.dig('metadata', 'name') == 'patchworks-kubefaas-builder' }
  builder.dig('spec', 'template', 'spec', 'containers').find { |c| c['name'] == 'dind' }
end
AUTO = 'mtu="$(cat /sys/class/net/eth0/mtu 2>/dev/null)"; exec dockerd-entrypoint.sh ${mtu:+--mtu="$mtu"}'

auto = dind
check(['sh', '-c'], auto['command'], 'dind command')
check([AUTO], auto['args'], 'dind args matching eth0')
_, error, status = Open3.capture3('sh', '-n', '-c', auto['args'].first)
raise "dind script does not parse: #{error}" unless status.success?

check(['exec dockerd-entrypoint.sh --mtu=1400'], dind('kubefaas.builder.dind.mtu=1400')['args'], 'dind args with mtu set')
# Values without the key, as under helm upgrade --reuse-values from an older release.
check([AUTO], dind('kubefaas.builder.dind.mtu=null')['args'], 'dind args without mtu')

puts 'kubefaas_dind_mtu_test.rb: ok'
