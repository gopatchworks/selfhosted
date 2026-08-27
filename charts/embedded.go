package charts

import "embed"

// FS contains the Patchworks Helm charts used by the installer.
//
//go:embed patchworks-infra/Chart.yaml patchworks-infra/Chart.lock patchworks-infra/README.md patchworks-infra/values.yaml patchworks-infra/charts/*.tgz patchworks-infra/docs/** patchworks-infra/files/** patchworks-infra/templates/**
//go:embed patchworks-app/Chart.yaml patchworks-app/README.md patchworks-app/values.yaml patchworks-app/docs/** patchworks-app/files/** patchworks-app/templates/**
var FS embed.FS
