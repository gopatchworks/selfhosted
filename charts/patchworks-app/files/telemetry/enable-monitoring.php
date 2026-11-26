<?php

/*
 * Turns on Core's platform-telemetry company feature, which shows the
 * Dashboard's Monitoring page, for every company with an initialised database.
 *
 * Run by the telemetry setup hook Job (templates/telemetry-setup.yaml) from
 * Core's application directory: php /opt/patchworks-telemetry/enable-monitoring.php
 *
 * A Core image without platform telemetry has no such feature, and is left
 * unchanged.
 */

declare(strict_types=1);

use App\Models\Fabric\Company;
use Laravel\Pennant\Feature;

$core = getcwd();
require $core . '/vendor/autoload.php';
$app = require $core . '/bootstrap/app.php';
$app->make(Illuminate\Contracts\Console\Kernel::class)->bootstrap();

$feature = 'platform-telemetry';

if (! in_array($feature, config('pennant.dashboard-features', []), true)) {
    fwrite(STDOUT, "[telemetry] This Core image has no {$feature} feature; the Monitoring page needs a Core image with platform telemetry.\n");
    exit(0);
}

$enabled = 0;
$already = 0;

Company::query()
    ->select(['id', 'name'])
    ->hasInitialisedDatabase()
    ->chunkById(100, function ($companies) use ($feature, &$enabled, &$already): void {
        foreach ($companies as $company) {
            if (Feature::for($company)->active($feature)) {
                $already++;

                continue;
            }

            Feature::for($company)->activate($feature);
            $enabled++;
        }
    });

fwrite(STDOUT, sprintf("[telemetry] Monitoring page turned on for %d companies (%d already had it).\n", $enabled, $already));
