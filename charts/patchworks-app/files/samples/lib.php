<?php

declare(strict_types=1);

namespace PatchworksSamples;

use App\Actions\Tenant\CheckConnectorAllowanceAction;
use App\Actions\Tenant\CreateFlowAction;
use App\Actions\Tenant\FlowGraph\CreateFlowStepAction;
use App\Ai\Tools\AddFilter;
use App\Ai\Tools\AddStep;
use App\Ai\Tools\ConnectStep;
use App\Ai\Tools\CreateFlowTool;
use App\Ai\Tools\SaveMapTool;
use App\Models\Fabric\Company;
use App\Models\Fabric\User;
use App\Models\Tenant\Flow;
use App\Models\Tenant\FlowFolder;
use App\Models\Tenant\FlowStep;
use App\Models\Tenant\FlowStepRoute;
use App\Models\Tenant\FlowVersion;
use App\Services\Tenant\FlowConnectionService;
use App\Services\Tenant\FlowGraphService;
use App\Services\Tenant\FlowTriggerUpgradeService;
use Illuminate\Support\Facades\Auth;
use Laravel\Ai\Tools\Request as ToolRequest;
use RuntimeException;
use Throwable;

/**
 * Builds sample flows with Core's own flow-builder classes (app/Ai/Tools), the
 * code path Core's flow builder agent uses, so every step is validated as the
 * dashboard validates it.
 *
 * Never destructive: a flow that already exists in the samples folder is left
 * alone, so edits and run history survive upgrades. Delete a sample to have the
 * next install or upgrade create it again.
 */
final class Samples
{
    public int $created = 0;

    public int $skipped = 0;

    public int $undeployed = 0;

    /** @var list<string> */
    public array $failures = [];

    private int $folderId;

    public function __construct(
        public readonly Company $company,
        public readonly User $user,
        string $folderName,
        public readonly string $connectorUrl,
    ) {
        $company->makeCurrent();
        Auth::setUser($user);
        $this->folderId = FlowFolder::firstOrCreate(['name' => $folderName])->id;
    }

    /** Resolve the seeded company and its admin from the Job's environment. */
    public static function fromEnvironment(): self
    {
        $companyName = (string) getenv('SAMPLES_COMPANY_NAME');
        $userEmail = (string) getenv('SAMPLES_USER_EMAIL');
        if ($companyName === '' || $userEmail === '') {
            throw new RuntimeException('SAMPLES_COMPANY_NAME and SAMPLES_USER_EMAIL must be set (seeds.tenant.companyName and seeds.tenant.adminEmail).');
        }
        $company = Company::where('name', $companyName)->orderBy('id')->first();
        if ($company === null) {
            throw new RuntimeException("No company named \"{$companyName}\". Samples install into the company the chart seeded.");
        }
        $user = User::where('email', $userEmail)->where('company_id', $company->id)->first();
        if ($user === null) {
            throw new RuntimeException("No user {$userEmail} in company \"{$companyName}\".");
        }

        return new self($company, $user, getenv('SAMPLES_FOLDER') ?: 'Samples', (string) getenv('SAMPLES_CONNECTOR_URL'));
    }

    public static function log(string $message): void
    {
        fwrite(STDOUT, '[samples] ' . $message . PHP_EOL);
    }

    /**
     * Create, lay out and deploy one sample flow unless it already exists.
     *
     * @param  callable(int $versionId, int $triggerId, int $flowId): void  $build
     * @return int|null the flow's id, or null when it could not be built
     */
    public function flow(string $name, string $description, callable $build): ?int
    {
        $existing = Flow::where('name', $name)->where('flow_folder_id', $this->folderId)->first();
        if ($existing !== null) {
            $this->skipped++;
            self::log("exists, left unchanged: {$name}");

            return $existing->id;
        }

        $flowId = null;
        try {
            $created = $this->call(new CreateFlowTool($this->user, app(CreateFlowAction::class)), ['name' => $name, 'description' => $description]);
            $flowId = (int) $created['flow_id'];
            $versionId = (int) $created['flow_version_id'];
            Flow::whereKey($flowId)->update(['flow_folder_id' => $this->folderId]);
            $version = FlowVersion::findOrFail($versionId);
            // The dashboard adds the Trigger step when it first loads a flow's graph.
            app(FlowTriggerUpgradeService::class)->upgradeIfNeeded($version);
            // CreateFlowAction gives every flow an hourly schedule: samples run only when started.
            $version->schedules()->delete();
            $triggerId = (int) FlowStep::where('flow_version_id', $versionId)->where('microservice', 'trigger')->value('id');

            $build($versionId, $triggerId, $flowId);

            $this->layout($versionId);
            try {
                $this->deploy($versionId);
                self::log("created: {$name}");
            } catch (AllowanceRefused $e) {
                // A licensing limit, not a broken sample: keep it as a draft to deploy later.
                $this->undeployed++;
                self::log("created, left undeployed: {$name}: {$e->getMessage()}");
            }
            $this->created++;

            return $flowId;
        } catch (Throwable $e) {
            // Remove only the half-built flow this run created, so the next run builds it again.
            if ($flowId !== null) {
                Flow::whereKey($flowId)->first()?->delete();
            }
            $this->failures[] = "{$name}: {$e->getMessage()}";
            self::log("FAILED: {$name}: {$e->getMessage()}");

            return null;
        }
    }

    public function step(int $versionId, string $name, string $microservice, array $config = [], ?int $parentId = null, ?string $route = null): int
    {
        $args = ['name' => $name, 'microservice' => $microservice, 'config' => $config];
        if ($parentId) {
            $args['parent_step_id'] = $parentId;
        }
        if ($route !== null) {
            $args['route_name'] = $route;
        }

        return (int) $this->call(new AddStep(FlowVersion::findOrFail($versionId), app(CreateFlowStepAction::class), $this->user), $args)['step_id'];
    }

    public function manualPayload(int $versionId, string $payload, int $parentId, ?string $route = null, string $format = 'json', string $name = 'Manual payload'): int
    {
        return $this->step($versionId, $name, 'manual-payload', ['payload' => $payload, 'payload_format' => $format], $parentId, $route);
    }

    public function assert(int $versionId, string $expected, int $parentId, ?string $route = null, string $name = 'Assert'): int
    {
        return $this->step($versionId, $name, 'assert-payload', ['expected_payload' => $expected], $parentId, $route);
    }

    /**
     * Name a Route or Branch step's branches. Core creates two when the step is
     * created ("First route"/"Second route", "First Branch"/"Second Branch");
     * those are renamed in order and further ones added after them.
     *
     * @param  list<string>  $names
     * @return array<string, int> route id per name
     */
    public function routes(int $stepId, array $names, string $logic = 'and'): array
    {
        $existing = FlowStepRoute::where('flow_step_id', $stepId)->orderBy('order')->orderBy('id')->get()->values();
        $ids = [];
        foreach (array_values($names) as $i => $name) {
            $route = $existing[$i] ?? null;
            if ($route !== null) {
                $route->update(['name' => $name, 'filter_logic_operator' => $logic]);
            } else {
                $max = (int) FlowStepRoute::where('flow_step_id', $stepId)->max('order');
                $route = FlowStepRoute::create(['flow_step_id' => $stepId, 'name' => $name, 'filter_logic_operator' => $logic, 'order' => $max + 1]);
            }
            $ids[$name] = $route->id;
        }

        return $ids;
    }

    /** @param  array{step_id?: int, route_id?: int}  $target */
    public function filter(int $versionId, array $target, array $filter): void
    {
        $this->call(new AddFilter(FlowVersion::findOrFail($versionId), $this->user), $target + $filter);
    }

    /** Add $sourceId as a further source of a Merge step. */
    public function connect(int $versionId, int $targetId, int $sourceId): void
    {
        $this->call(new ConnectStep(FlowVersion::findOrFail($versionId), app(FlowConnectionService::class), $this->user), ['target_step_id' => $targetId, 'parent_step_id' => $sourceId]);
    }

    /** Save a map step's mapping from Agent Map Language, as the flow builder does. */
    public function saveMap(int $versionId, int $flowId, int $stepId, string $aml): void
    {
        $tool = new SaveMapTool(FlowStep::findOrFail($stepId), FlowVersion::findOrFail($versionId), Flow::findOrFail($flowId), $this->user);
        $raw = (string) $tool->handle(new ToolRequest(['aml' => $aml]));
        $decoded = json_decode($raw, true);
        if (! is_array($decoded) || isset($decoded['error'])) {
            throw new RuntimeException('Saving the map failed: ' . $raw);
        }
    }

    /** Lay the version out as the dashboard's "Auto layout" does, before it is deployed and copied. */
    public function layout(int $versionId): void
    {
        $version = FlowVersion::findOrFail($versionId);
        $graph = json_decode(json_encode(app(FlowGraphService::class)->buildGraph($version, false)), true);
        $nodes = array_values(array_filter($graph['nodes'] ?? [], fn ($n) => ($n['type'] ?? '') !== 'flow-note'));
        $direction = Flow::whereKey($version->flow_id)->value('layout_direction') ?: 'TB';
        foreach (Layout::positions($nodes, $graph['edges'] ?? [], (string) $direction) as $id => $position) {
            FlowStep::where('flow_version_id', $versionId)->whereKey((int) $id)->update(['position_x' => $position['x'], 'position_y' => $position['y']]);
        }
    }

    /** Deploy as FlowVersionDeploymentController::store does, connector allowance included. */
    public function deploy(int $versionId): void
    {
        $version = FlowVersion::findOrFail($versionId);
        $refusal = app(CheckConnectorAllowanceAction::class)($version);
        if ($refusal !== null) {
            throw new AllowanceRefused($refusal);
        }
        $version->deployments()->create([
            'flow_version_id' => $version->id,
            'enabled_at' => now(),
            'enabled_by' => $this->user->id,
            'user_id' => $this->user->id,
        ]);
        if ($version->saved_at === null) {
            $version->update(['saved_at' => now(), 'saved_by' => $this->user->id]);
        }
    }

    /** Whether the flow version could be deployed under the company's connector allowance. */
    public function connectorsAllowed(int $versionId): bool
    {
        return app(CheckConnectorAllowanceAction::class)(FlowVersion::findOrFail($versionId)) === null;
    }

    private function call(object $tool, array $args): array
    {
        $raw = (string) $tool->handle(new ToolRequest($args));
        $decoded = json_decode($raw, true);
        if (! is_array($decoded) || isset($decoded['error'])) {
            throw new RuntimeException(class_basename($tool) . ': ' . $raw);
        }

        return $decoded;
    }
}

/** Core refused to deploy a sample because of a company allowance. */
final class AllowanceRefused extends RuntimeException {}
