# Run: ruby tests/kubefaas_namespace_test.rb (Helm required; no cluster access).
#
# The infra chart creates the KubeFaaS namespace, but never the release
# namespace. That one exists before Helm installs anything, and whoever made it
# keeps it: a release that renders it under Argo CD tracks it, so pruning the
# app deletes the namespace, and its apply removed a label Kargo needed
# (kargo.akuity.io/project) when KubeFaaS shared the release namespace.
require 'yaml'
require 'open3'
CHART = File.expand_path('../charts/patchworks-infra', __dir__)
def check(expected, actual, what)
  raise "#{what}: expected #{expected.inspect}, got #{actual.inspect}" unless expected == actual
end
def namespaces(*sets)
  args = ['helm', 'template', 'test', CHART, '--namespace', 'patchworks', '--set', 'kubefaas.enabled=true']
  sets.each { |s| args += ['--set', s] }
  output, error, status = Open3.capture3(*args)
  raise error unless status.success?
  docs = YAML.load_stream(output).compact
  [docs.select { |d| d['kind'] == 'Namespace' }.map { |d| d.dig('metadata', 'name') }.sort, docs]
end
def in_namespace(docs, kind, name)
  docs.find { |d| d['kind'] == kind && d.dig('metadata', 'name') == name }&.dig('metadata', 'namespace')
end

names, = namespaces
check(%w[kubefaas kubefaas-fn-01 kubefaas-fn-02 kubefaas-fn-03 kubefaas-fn-04 kubefaas-fn-05], names, 'namespaces with the default KubeFaaS namespace')

names, docs = namespaces('kubefaas.namespace=patchworks')
check(false, names.include?('patchworks'), 'release namespace rendered as a KubeFaaS namespace')
check(true, names.any? { |n| n.start_with?('kubefaas-fn-') }, 'function namespaces still rendered')
# KubeFaaS itself still goes into the shared namespace.
check('patchworks', in_namespace(docs, 'Deployment', 'patchworks-kubefaas-controller'), 'controller namespace')
check('patchworks', in_namespace(docs, 'Deployment', 'patchworks-kubefaas-builder'), 'builder namespace')

# A KubeFaaS namespace that is only the app namespace (.Values.namespace), not
# the release's, is still created: nothing else creates it before the install.
names, = namespaces('kubefaas.namespace=apps', 'namespace=apps')
check(true, names.include?('apps'), 'KubeFaaS namespace shared with a non-release app namespace')

puts 'kubefaas_namespace_test.rb: ok'
