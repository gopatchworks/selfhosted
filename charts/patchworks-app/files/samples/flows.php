<?php

declare(strict_types=1);

namespace PatchworksSamples;

use App\Ai\Tools\CreateCacheTool;
use App\Ai\Tools\CreateCrossReferenceLookupTool;
use App\Models\EntityType;
use App\Models\Tenant\AuthenticationImplementation;
use App\Models\Tenant\Cache;
use App\Models\Tenant\Connector;
use App\Models\Tenant\CrossReferenceLookup;
use App\Models\Tenant\DataPool;
use App\Models\Tenant\Endpoint;
use App\Models\Tenant\Flow;
use App\Models\Tenant\FlowVersionVariable;
use App\Models\Tenant\Script;
use App\Models\Tenant\System;
use Illuminate\Support\Facades\DB;
use Laravel\Ai\Tools\Request as ToolRequest;
use RuntimeException;

/**
 * The sample flows. Each starts from a Manual Payload, runs one shape (or a
 * pair that only make sense together) and ends in an Assert on the exact
 * output, so a successful run shows the shape produced the right data. "Sample:
 * Every shape" runs every shape in one flow.
 *
 * Samples are deployed but never started here and have no schedule.
 */
final class Flows
{
    private const ORDERS = '[{"id":1,"status":"Completed"},{"id":2,"status":"Pending"},{"id":3,"status":"Completed"}]';

    private const COMPLETED = '[{"id":1,"status":"Completed"},{"id":3,"status":"Completed"}]';

    private const MAP_AML = <<<'AML'
src "id" as "ID"
src "customer_name" as "Customer name"
out "id" -> "order_id" as "Order ID"
out "customer_name" -> "customer.name" as "Customer name"
AML;

    private const SCRIPT = <<<'PHP'
<?php

// Sample script: marks the payload as scripted. The sample steps that use it are
// skipped until the script is deployed, which needs KubeFaaS.
function handle($data)
{
    $payload = json_decode($data['payload'], true) ?: [];
    $payload['scripted'] = true;
    $data['payload'] = json_encode($payload);

    return $data;
}
PHP;

    private ?int $cacheId = null;

    private ?int $poolId = null;

    private ?int $lookupId = null;

    private ?array $connector = null;

    private int|string|null $scriptVersionId = null;

    private ?int $flowReturnChildId = null;

    public function __construct(private readonly Samples $s) {}

    public function install(): void
    {
        $s = $this->s;
        $s->flow('Sample: Manual Payload + Assert', 'Manual Payload then an Assert on the same payload.', function ($v, $t) {
            $this->s->assert($v, '{"id":1,"name":"widget"}', $this->s->manualPayload($v, '{"id":1,"name":"widget"}', $t));
        });
        $s->flow('Sample: Filter', 'Keeps orders whose status is Completed.', fn ($v, $t) => $this->filter($v, $this->s->manualPayload($v, self::ORDERS, $t)));
        $s->flow('Sample: Route', 'Sends Completed and Pending orders down separate routes.', function ($v, $t) {
            $rt = $this->s->step($v, 'Route by status', 'route', ['payload_format' => 'json', 'route_behaviour' => 'all'], $this->s->manualPayload($v, self::ORDERS, $t));
            $routes = $this->s->routes($rt, ['Completed', 'Pending']);
            $this->statusFilter($v, ['route_id' => $routes['Completed']], 'Completed');
            $this->statusFilter($v, ['route_id' => $routes['Pending']], 'Pending');
            $this->s->assert($v, self::COMPLETED, $rt, 'Completed', 'Assert completed');
            $this->s->assert($v, '[{"id":2,"status":"Pending"}]', $rt, 'Pending', 'Assert pending');
        });
        $s->flow('Sample: Split', 'Splits out customer.addresses, wrapped in "addresses".', fn ($v, $t) => $this->split($v, $t));
        $s->flow('Sample: Flow Control', 'Batches 4 items in twos: each batch reaches the Assert as its own payload of 2 items.', fn ($v, $t) => $this->flowControl($v, $t));
        $s->flow('Sample: Data Transformation', 'CSV to JSON, then Find/Replace Widget with Gadget, one function per step.', fn ($v, $t) => $this->dataTransformation($v, $t));
        $s->flow('Sample: Branch + Merge', 'Two branches each make a payload; Merge waits for both and passes both on to the Assert.', fn ($v, $t) => $this->branchMerge($v, $t));
        $s->flow('Sample: Try/Catch', 'An Assert that cannot match fails on Try, so the run ends in partial success. Assert runs outside Core\'s per-payload loop, so Catch receives {} with the error in meta, as in Core.', function ($v, $t) {
            $tc = $this->s->step($v, 'Try/Catch', 'try-catch', ['action' => 'partial_success'], $this->s->manualPayload($v, '{"id":7,"status":"new"}', $t));
            $this->s->assert($v, '{"id":7,"status":"never"}', $tc, 'Try', 'Assert mismatch (fails on purpose)');
            $this->s->assert($v, '{}', $tc, 'Catch', 'Assert caught payload');
        });
        $s->flow('Sample: Add to Cache + Load From Cache', 'Caches a payload, replaces it, then loads the cached copy back.', fn ($v, $t, $f) => $this->cache($v, $t, $f));
        $s->flow('Sample: De-Dupe', 'Branch 1 tracks sku "seen"; branch 2 filters a payload holding "seen" and "fresh", keeping only "fresh".', function ($v, $t) {
            $br = $this->s->step($v, 'Branch', 'branch', ['use_route_order' => true], $this->s->manualPayload($v, '{"start":true}', $t));
            $this->s->routes($br, ['Track', 'Filter']);
            $this->dedupeTrack($v, $this->s->manualPayload($v, '{"items":[{"sku":"seen"}]}', $br, 'Track', name: 'Seen payload'));
            $this->dedupeFilter($v, $this->s->manualPayload($v, '{"items":[{"sku":"seen"},{"sku":"fresh"}]}', $br, 'Filter', name: 'Seen and fresh'));
        });
        $s->flow('Sample: Track Data', 'Tracks the sku of a payload and passes it on unchanged.', function ($v, $t) {
            $this->s->assert($v, '{"sku":"TRACK-1","name":"Tracked"}', $this->track($v, $this->s->manualPayload($v, '{"sku":"TRACK-1","name":"Tracked"}', $t)));
        });
        $s->flow('Sample: Cross-Reference', 'Looks up A1 and B2 in a lookup table; Z9 has no match and passes through.', fn ($v, $t) => $this->crossReference($v, $this->s->manualPayload($v, '["A1","B2","Z9"]', $t)));
        $s->flow('Sample: Set Variables', 'Sets a flow variable and a meta variable from the payload, then filters with each on its own route.', function ($v, $t) {
            $rt = $this->setVariablesAndSplit($v, $t, '{"want":"B","items":[{"code":"A"},{"code":"B"}]}', 'items');
            $routes = $this->s->routes($rt, ['Flow variable', 'Meta variable']);
            $this->s->filter($v, ['route_id' => $routes['Flow variable']], ['type' => 'string', 'field' => 'code', 'comparison' => '==', 'value' => '{{flow.variables.wanted}}', 'keep_matching' => true]);
            $this->s->filter($v, ['route_id' => $routes['Meta variable']], ['type' => 'string', 'field' => 'code', 'comparison' => '==', 'value' => '[[meta.wanted_meta]]', 'keep_matching' => true]);
            $this->s->assert($v, '[{"code":"B"}]', $rt, 'Flow variable', 'Assert flow variable');
            $this->s->assert($v, '[{"code":"B"}]', $rt, 'Meta variable', 'Assert meta variable');
        });
        $s->flow('Sample: Notify', 'Logs a notification whose content uses the payload, on the log channel.', function ($v, $t) {
            $this->s->assert($v, '{"id":42}', $this->notify($v, $this->s->manualPayload($v, '{"id":42}', $t), 'Sample notification for order [[payload.id]]'));
        });

        $childId = $s->flow('Sample: Run Process Flow (child)', 'Started by "Sample: Run Process Flow"; asserts the payload it receives.', function ($v, $t) {
            $this->s->assert($v, '{"from":"parent"}', $t, null, 'Assert parent payload');
        });
        if ($childId !== null) {
            $s->flow('Sample: Run Process Flow', 'Starts a child flow with this payload without waiting. The child is queued for the scheduler, so it runs only while the company has flow concurrency allowance.', function ($v, $t) use ($childId) {
                $this->s->step($v, 'Run child flow', 'run-flow', ['flow_id' => $childId, 'latest_deployed' => true], $this->s->manualPayload($v, '{"from":"parent"}', $t));
            });
        }
        $s->flow('Sample: Run Process Flow (wait) + Flow Return', 'Waits for a child flow that rewrites the payload and hands it back with Flow Return.', fn ($v, $t) => $this->runFlowWait($v, $this->s->manualPayload($v, '{"from":"parent"}', $t)));
        $s->flow('Sample: Map', 'Maps id to order_id and customer_name to customer.name.', fn ($v, $t, $f) => $this->map($v, $f, $this->s->manualPayload($v, '{"id":1,"customer_name":"Bob Smith"}', $t)));
        $connectorFlow = $s->flow('Sample: Connector', 'GETs the Monocore hub health endpoint inside the cluster through a no-auth custom system. Deploying it needs a free connector allowance.', fn ($v, $t) => $this->connector($v, $t));
        // Every shape includes the Connector only where the company may deploy it, so it can always run.
        $connectorsAllowed = $connectorFlow !== null && Flow::find($connectorFlow)?->deployedVersion !== null;
        $callbackFlow = $s->flow('Sample: Callback', 'Call its callback URL with {"msg":"ping"}: it rewrites ping to pong and answers 201 with the payload. Callbacks are limited by the company\'s callback allowance.', function ($v, $t) {
            $rewrite = $this->s->step($v, 'Rewrite', 'data-transform', ['transformations' => [$this->transformation('find_replace', ['find' => 'ping', 'replace' => 'pong'])]], $t);
            $this->s->step($v, 'Callback', 'callback', ['response_code' => 201, 'return_first_payload' => true], $rewrite);
        });
        if ($callbackFlow !== null && ! Flow::find($callbackFlow)?->callbacks()->exists()) {
            Flow::findOrFail($callbackFlow)->callbacks()->create(['status' => 201, 'format' => 'json']);
        }
        $s->flow('Sample: Script', 'A Script step, skipped until its script is deployed, which needs KubeFaaS. A skipped step passes its payload on.', function ($v, $t) {
            $this->s->assert($v, '{"id":5}', $this->script($v, $this->s->manualPayload($v, '{"id":5}', $t)));
        });

        if (! $connectorsAllowed) {
            Samples::log('the company has no connector allowance left, so "Sample: Every shape" leaves out its Connector branch');
        }
        $s->flow('Sample: Every shape', 'Every shape in one flow: each branch runs shapes and ends in an Assert. Branches run in order and a failure skips the rest, so a successful run means every shape gave the expected data.', fn ($v, $t, $f) => $this->everyShape($v, $t, $f, $connectorsAllowed));
    }

    private function everyShape(int $v, int $t, int $flowId, bool $withConnector): void
    {
        $s = $this->s;
        $orders = '[{"id":1,"sku":"A1","status":"Completed"},{"id":2,"sku":"B2","status":"Pending"},{"id":3,"sku":"Z9","status":"Completed"}]';
        $completed = '[{"id":1,"sku":"A1","status":"Completed"},{"id":3,"sku":"Z9","status":"Completed"}]';

        $rt = $this->setVariablesAndSplit($v, $t, '{"want":"Completed","orders":' . $orders . '}', 'orders');
        $routes = $s->routes($rt, ['Completed orders', 'Every other shape']);
        $s->filter($v, ['route_id' => $routes['Completed orders']], ['type' => 'string', 'field' => 'status', 'comparison' => '==', 'value' => '{{flow.variables.wanted}}', 'keep_matching' => true]);
        $s->assert($v, $completed, $rt, 'Completed orders', 'Assert flow variable route');

        $br = $s->step($v, 'Branch', 'branch', ['use_route_order' => true], $rt, 'Every other shape');
        $branches = ['Filter', 'Flow Control', 'Split and Map', 'Data Transformation', 'Cache', 'De-Dupe', 'Track, Notify, Cross-Reference', 'Connector', 'Run Process Flow', 'Branch and Merge', 'Try/Catch', 'Callback', 'Script'];
        if (! $withConnector) {
            $branches = array_values(array_diff($branches, ['Connector']));
        }
        $s->routes($br, $branches);

        // Filter on the meta variable Set Variables wrote.
        $fl = $s->step($v, 'Filter on meta variable', 'filter', ['payload_format' => 'json'], $br, 'Filter');
        $s->filter($v, ['step_id' => $fl], ['type' => 'string', 'field' => 'status', 'comparison' => '==', 'value' => '[[meta.wanted_meta]]', 'keep_matching' => true]);
        $s->assert($v, $completed, $fl);

        $this->flowControl($v, $br, 'Flow Control');

        $this->map($v, $flowId, $s->manualPayload($v, '{"id":1,"customer_name":"Bob Smith"}', $this->split($v, $br, 'Split and Map')));

        $this->dataTransformation($v, $br, 'Data Transformation');

        $this->cache($v, $br, $flowId, 'Cache');

        $seen = $s->manualPayload($v, '{"items":[{"sku":"seen"}]}', $br, 'De-Dupe', name: 'Seen payload');
        $this->dedupeFilter($v, $s->manualPayload($v, '{"items":[{"sku":"seen"},{"sku":"fresh"}]}', $this->dedupeTrack($v, $seen), name: 'Seen and fresh'));

        $tracked = $this->track($v, $s->manualPayload($v, '{"sku":"TRACK-1","name":"Tracked"}', $br, 'Track, Notify, Cross-Reference'));
        $notified = $this->notify($v, $tracked, 'Every shape sample tracked [[payload.sku]]');
        $this->crossReference($v, $s->manualPayload($v, '["A1","B2","Z9"]', $notified));

        if ($withConnector) {
            $this->connector($v, $br, 'Connector');
        }

        $this->runFlowWait($v, $s->manualPayload($v, '{"from":"parent"}', $br, 'Run Process Flow'));

        $this->branchMerge($v, $br, 'Branch and Merge');

        $tc = $s->step($v, 'Try/Catch', 'try-catch', ['action' => 'partial_success'], $br, 'Try/Catch');
        $s->assert($v, $orders, $tc, 'Try', 'Assert inside Try');
        $s->step($v, 'Notify caught error', 'notify', ['channel' => 'log', 'level' => 'warning', 'content' => 'Caught: [[meta.error_message]]'], $tc, 'Catch');

        // A manual run passes the payload through Callback; a callback-triggered run answers with it.
        $s->assert($v, $orders, $s->step($v, 'Callback', 'callback', ['response_code' => 200, 'return_first_payload' => true], $br, 'Callback'));

        $s->assert($v, $orders, $this->script($v, $br, 'Script'));
    }

    private function statusFilter(int $v, array $target, string $status): void
    {
        $this->s->filter($v, $target, ['type' => 'string', 'field' => 'status', 'comparison' => '==', 'value' => $status, 'keep_matching' => true]);
    }

    private function filter(int $v, int $parent, ?string $route = null): void
    {
        $fl = $this->s->step($v, 'Filter completed', 'filter', ['payload_format' => 'json'], $parent, $route);
        $this->statusFilter($v, ['step_id' => $fl], 'Completed');
        $this->s->assert($v, self::COMPLETED, $fl);
    }

    /** Returns the Assert after the split, for a further step to follow. */
    private function split(int $v, int $parent, ?string $route = null): int
    {
        $mp = $this->s->manualPayload($v, '{"customer":{"name":"Ann","addresses":[{"line":"1 High St"},{"line":"2 Low Rd"}]}}', $parent, $route);
        $sp = $this->s->step($v, 'Split addresses', 'split', ['level_to_split' => 'customer.addresses', 'payload_format' => 'json', 'wrapper_key' => 'addresses'], $mp);

        return $this->s->assert($v, '{"addresses":[{"line":"1 High St"},{"line":"2 Low Rd"}]}', $sp, null, 'Assert split');
    }

    private function flowControl(int $v, int $parent, ?string $route = null): void
    {
        // Identical items, so every batch of 2 matches the one Assert; unbatched, the payload would be the whole object.
        $mp = $this->s->manualPayload($v, '{"items":[{"sku":"A1"},{"sku":"A1"},{"sku":"A1"},{"sku":"A1"}]}', $parent, $route);
        $b = $this->s->step($v, 'Batch in twos', 'batch', ['path' => 'items', 'size' => 2, 'payload_format' => 'json'], $mp);
        $this->s->assert($v, '[{"sku":"A1"},{"sku":"A1"}]', $b);
    }


    private function dataTransformation(int $v, int $parent, ?string $route = null): void
    {
        $mp = $this->s->manualPayload($v, "id,name\n1,Widget\n", $parent, $route, 'plain');
        $csv = $this->s->step($v, 'CSV to JSON', 'data-transform', ['transformations' => [$this->transformation('csv_to_json', (object) [])]], $mp);
        $fr = $this->s->step($v, 'Find/Replace', 'data-transform', ['transformations' => [$this->transformation('find_replace', ['find' => 'Widget', 'replace' => 'Gadget'])]], $csv);
        $this->s->assert($v, '[{"id":"1","name":"Gadget"}]', $fr);
    }

    private function branchMerge(int $v, int $parent, ?string $route = null): void
    {
        $br = $this->s->step($v, $route === null ? 'Branch' : 'Nested branch', 'branch', ['use_route_order' => true], $route === null ? $this->s->manualPayload($v, '{"start":true}', $parent) : $parent, $route);
        $this->s->routes($br, ['First', 'Second']);
        // Both branches make the same payload, so the Assert checks each one Merge passes on.
        $a = $this->s->manualPayload($v, '{"merged":true}', $br, 'First', name: 'First payload');
        $b = $this->s->manualPayload($v, '{"merged":true}', $br, 'Second', name: 'Second payload');
        $m = $this->s->step($v, 'Merge', 'merge', ['mode' => 'passthrough'], $a);
        $this->s->connect($v, $m, $b);
        $this->s->assert($v, '{"merged":true}', $m);
    }

    private function cache(int $v, int $parent, int $flowId, ?string $route = null): void
    {
        $cacheId = $this->cacheId($flowId);
        $mp = $this->s->manualPayload($v, '{"sku":"A1","qty":3}', $parent, $route);
        $set = $this->s->step($v, 'Add to cache', 'cache-set', ['cache_id' => $cacheId, 'key' => 'sample-item'], $mp);
        $other = $this->s->manualPayload($v, '{"replaced":true}', $set, name: 'Replace payload');
        $get = $this->s->step($v, 'Load from cache', 'cache-get', ['cache_id' => $cacheId, 'key' => 'sample-item', 'fail_on_miss' => true], $other);
        $this->s->assert($v, '{"sku":"A1","qty":3}', $get);
    }

    private function dedupeTrack(int $v, int $parent): int
    {
        return $this->s->step($v, 'De-Dupe track', 'dedupe', ['mode' => 'track', 'key_field' => 'items.*.sku', 'data_pool_id' => $this->poolId(), 'entity_type_id' => $this->entityTypeId(), 'payload_format' => 'json'], $parent);
    }

    private function dedupeFilter(int $v, int $parent): void
    {
        $f = $this->s->step($v, 'De-Dupe filter', 'dedupe', ['mode' => 'filter', 'key_field' => 'items.*.sku', 'data_pool_id' => $this->poolId(), 'entity_type_id' => $this->entityTypeId(), 'payload_format' => 'json'], $parent);
        $this->s->assert($v, '{"items":[{"sku":"fresh"}]}', $f);
    }

    private function track(int $v, int $parent): int
    {
        return $this->s->step($v, 'Track sku', 'track', ['direction' => 'Send', 'entity_type_id' => $this->entityTypeId(), 'tracked_fields' => [['value' => 'sku', 'useKeyAsValue' => false]], 'payload_format' => 'json', 'message' => 'sample'], $parent);
    }

    private function notify(int $v, int $parent, string $content): int
    {
        return $this->s->step($v, 'Notify', 'notify', ['channel' => 'log', 'level' => 'info', 'content' => $content], $parent);
    }

    private function crossReference(int $v, int $parent): void
    {
        $x = $this->s->step($v, 'Cross-reference SKUs', 'cross-reference', ['behaviour' => 'lookup', 'cross_reference_lookup_id' => $this->lookupId(), 'direction' => 'left-to-right', 'key_field' => '*', 'target_field' => '*', 'match_type' => 'exact', 'output_type' => 'string', 'payload_format' => 'json'], $parent);
        $this->s->assert($v, '["WIDGET","GADGET","Z9"]', $x);
    }

    /** Manual Payload, Set Variables (flow variable "wanted", meta "wanted_meta"), Split, then a Route; returns the Route. */
    private function setVariablesAndSplit(int $v, int $t, string $payload, string $splitLevel): int
    {
        $var = FlowVersionVariable::create(['flow_version_id' => $v, 'type' => 'string', 'key' => 'wanted', 'value' => '']);
        $mp = $this->s->manualPayload($v, $payload, $t);
        $sv = $this->s->step($v, 'Set variables', 'variable-set', ['variables' => [['variable' => $var->id, 'value' => '[[payload.want]]']], 'meta' => [['key' => 'wanted_meta', 'value' => '[[payload.want]]']]], $mp);
        $sp = $this->s->step($v, 'Split ' . $splitLevel, 'split', ['level_to_split' => $splitLevel, 'payload_format' => 'json'], $sv);

        return $this->s->step($v, 'Route', 'route', ['payload_format' => 'json', 'route_behaviour' => 'all'], $sp);
    }

    private function runFlowWait(int $v, int $parent): void
    {
        $rf = $this->s->step($v, 'Run child and wait', 'run-flow', ['flow_id' => $this->flowReturnChildId(), 'latest_deployed' => true, 'wait_for_completion' => true, 'timeout_seconds' => 120], $parent);
        $this->s->assert($v, '{"from":"child"}', $rf);
    }

    private function map(int $v, int $flowId, int $parent): void
    {
        $map = $this->s->step($v, 'Map order', 'map', ['source_format_override' => 'json', 'destination_format_override' => 'json'], $parent);
        $this->s->saveMap($v, $flowId, $map, self::MAP_AML);
        $this->s->assert($v, '{"order_id":1,"customer":{"name":"Bob Smith"}}', $map, null, 'Assert map');
    }

    private function connector(int $v, int $parent, ?string $route = null): void
    {
        [$connectorId, $endpointId] = $this->connectorIds();
        $c = $this->s->step($v, 'Get Monocore health', 'connector', ['connector_id' => $connectorId, 'endpoint_id' => $endpointId], $parent, $route);
        $this->s->assert($v, "{\"status\":\"ok\"}\n", $c);
    }

    private function script(int $v, int $parent, ?string $route = null): int
    {
        return $this->s->step($v, 'Script (skipped)', 'script', ['script_version_id' => $this->scriptVersionId(), 'skip' => true], $parent, $route);
    }

    private function transformation(string $key, array|object $parameters): array
    {
        $function = DB::table('transformation_functions')->where('key', $key)->orderByDesc('version')->first(['id', 'key', 'version']);
        if ($function === null) {
            throw new RuntimeException("Transformation function {$key} is not in the catalogue.");
        }

        return ['transformation_function_id' => $function->id, 'transformation_function_key' => $function->key, 'transformation_function_version' => $function->version, 'use_latest_version' => false, 'parameters' => $parameters];
    }

    private function entityTypeId(): int
    {
        return (int) (EntityType::where('name', 'customer')->value('id') ?? EntityType::orderBy('id')->value('id'));
    }

    private function cacheId(int $flowId): int
    {
        return $this->cacheId ??= (int) (Cache::where('name', 'Sample cache')->value('id')
            ?: $this->toolResult(new CreateCacheTool($this->s->user, Flow::findOrFail($flowId)), ['name' => 'Sample cache', 'scope' => 'Company', 'ttl' => 7200])['id']);
    }

    private function poolId(): int
    {
        return $this->poolId ??= DataPool::firstOrCreate(['name' => 'Sample data pool'], ['description' => 'Used by the De-Dupe samples'])->id;
    }

    private function lookupId(): int
    {
        if ($this->lookupId !== null) {
            return $this->lookupId;
        }
        if (! CrossReferenceLookup::where('name', 'Sample SKU map')->exists()) {
            $this->toolResult(new CreateCrossReferenceLookupTool($this->s->user), ['name' => 'Sample SKU map', 'description' => 'Used by the Cross-Reference samples', 'default_value' => null,
                'values' => [['left_value' => 'A1', 'right_value' => 'WIDGET'], ['left_value' => 'B2', 'right_value' => 'GADGET']]]);
        }

        return $this->lookupId = (int) CrossReferenceLookup::where('name', 'Sample SKU map')->value('id');
    }

    /** @return array{0: int, 1: int} connector and endpoint ids */
    private function connectorIds(): array
    {
        if ($this->connector !== null) {
            return $this->connector;
        }
        if ($this->s->connectorUrl === '') {
            throw new RuntimeException('SAMPLES_CONNECTOR_URL is not set.');
        }
        $systemTypeId = DB::table('system_types')->where('name', 'Other')->value('id');
        $authTypeId = DB::table('authentication_types')->where('name', 'PUBLIC')->value('id');
        if ($systemTypeId === null || $authTypeId === null) {
            throw new RuntimeException('System type "Other" or authentication type "PUBLIC" is missing from the catalogue.');
        }
        $system = System::firstOrCreate(['name' => 'Sample system'], ['label' => 'Sample system', 'colour' => '#4f46e5', 'version' => 1, 'version_name' => 'v1', 'system_type_id' => $systemTypeId, 'protocol' => 'HTTP', 'date_format' => 'Y-m-d\TH:i:sP']);
        $auth = AuthenticationImplementation::firstOrCreate(['system_id' => $system->id, 'name' => 'Public'], ['authentication_type_id' => $authTypeId, 'method' => 'GET', 'endpoint' => '', 'body_format' => 'none', 'data_type' => 'json']);
        $endpoint = Endpoint::firstOrCreate(['system_id' => $system->id, 'name' => 'Monocore health'], [
            'entity_type_id' => $this->entityTypeId(), 'endpoint' => $this->s->connectorUrl,
            'direction' => 'Receive', 'http_method' => 'GET', 'data_type' => 'json', 'returns_multiple_records' => false, 'body_format' => 'none',
        ]);
        $connector = Connector::firstOrCreate(['name' => 'Sample connector'], ['authentication_implementation_id' => $auth->id, 'timezone' => 'UTC', 'date_format' => 'Y-m-d\TH:i:sP', 'rate_limit_unit' => 'second', 'rate_limit_duration' => 0]);

        return $this->connector = [$connector->id, $endpoint->id];
    }

    private function scriptVersionId(): int|string
    {
        if ($this->scriptVersionId !== null) {
            return $this->scriptVersionId;
        }
        $script = Script::firstOrCreate(['name' => 'Sample script'], ['description' => 'Marks the payload as scripted. Deploy it (needs KubeFaaS) and un-skip the sample steps to run it.', 'language' => 'php_8_3']);
        $version = $script->versions()->orderBy('id')->first();
        if ($version === null) {
            $version = $script->versions()->create();
            $version->content()->create(['content' => self::SCRIPT]);
        }

        return $this->scriptVersionId = $version->id;
    }

    private function flowReturnChildId(): int
    {
        if ($this->flowReturnChildId !== null) {
            return $this->flowReturnChildId;
        }
        $id = $this->s->flow('Sample: Flow Return (child)', 'Started and awaited by the Run Process Flow (wait) samples; rewrites parent to child and returns the payload.', function ($v, $t) {
            $rewrite = $this->s->step($v, 'Rewrite', 'data-transform', ['transformations' => [$this->transformation('find_replace', ['find' => 'parent', 'replace' => 'child'])]], $t);
            $this->s->step($v, 'Flow return', 'flow-return', [], $rewrite);
        });
        if ($id === null) {
            throw new RuntimeException('The Flow Return child flow could not be created.');
        }

        return $this->flowReturnChildId = $id;
    }

    private function toolResult(object $tool, array $args): array
    {
        $decoded = json_decode((string) $tool->handle(new ToolRequest($args)), true);
        if (! is_array($decoded) || isset($decoded['error'])) {
            throw new RuntimeException(class_basename($tool) . ' failed: ' . json_encode($decoded));
        }

        return $decoded;
    }
}
