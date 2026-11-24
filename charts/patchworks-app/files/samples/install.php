<?php

/*
 * Installs the Patchworks sample flows into the company the chart seeded.
 *
 * Run by the samples hook Job (templates/samples.yaml) from Core's application
 * directory: php /opt/patchworks-samples/install.php
 *
 * Environment: SAMPLES_COMPANY_NAME, SAMPLES_USER_EMAIL, SAMPLES_FOLDER and
 * SAMPLES_CONNECTOR_URL, plus Core's usual application environment.
 *
 * Exits non-zero when a sample could not be created; samples that already
 * exist are left unchanged.
 */

declare(strict_types=1);

use PatchworksSamples\Flows;
use PatchworksSamples\Samples;

$core = getcwd();
require $core . '/vendor/autoload.php';
$app = require $core . '/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

require __DIR__ . '/layout.php';
require __DIR__ . '/lib.php';
require __DIR__ . '/flows.php';


try {
    $samples = Samples::fromEnvironment();
} catch (Throwable $e) {
    Samples::log('FAILED: ' . $e->getMessage());
    exit(1);
}

Samples::log(sprintf('installing into company "%s" as %s', $samples->company->name, $samples->user->email));
(new Flows($samples))->install();
Samples::log(sprintf('%d created (%d left undeployed), %d already present, %d failed', $samples->created, $samples->undeployed, $samples->skipped, count($samples->failures)));

exit($samples->failures === [] ? 0 : 1);
