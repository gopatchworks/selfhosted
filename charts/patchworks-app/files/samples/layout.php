<?php

declare(strict_types=1);

namespace PatchworksSamples;

/**
 * Port of the dashboard's auto layout (dashboard lib/flowGraphLayout.ts,
 * applyDfsLayout, with the 15px snap of useAutoLayout/usePositionTracking), so
 * sample flows open laid out as if "Auto layout" had been pressed.
 *
 * Nodes and edges are the ones Core's FlowGraphService::buildGraph returns to
 * the dashboard. Positions are vue-flow top-left corners, the frame Core stores
 * in flow_steps.position_x and position_y.
 *
 * @param  list<array{id: string, data?: array}>  $nodes
 * @param  list<array{source: string, target: string, sourceHandle?: ?string}>  $edges
 * @return array<string, array{x: int, y: int}> snapped top-left position per node id
 */
final class Layout
{
    public const NODE_WIDTH = 320;

    public const DEFAULT_NODE_HEIGHT = 100;

    private const H_SPACING = 50;

    private const V_SPACING = 80;

    private const BRANCH_STAGGER = 25;

    private const GRID = 15;

    public static function positions(array $nodes, array $edges, string $direction = 'TB'): array
    {
        $raw = self::dfs($nodes, $edges, $direction === 'LR' ? 'LR' : 'TB');
        $snapped = [];
        foreach ($raw as $id => $position) {
            $snapped[$id] = [
                'x' => self::snap($position['x']),
                'y' => self::snap($position['y']),
            ];
        }

        return $snapped;
    }

    /** Math.round(position / 15) * 15, with JavaScript's round-half-up. */
    private static function snap(float $value): int
    {
        return (int) (floor($value / self::GRID + 0.5) * self::GRID);
    }

    /** JavaScript's String.prototype.localeCompare for the numeric-string ids Core uses. */
    private static function compareIds(string $a, string $b): int
    {
        return strcmp($a, $b);
    }

    private static function edgeOrder(array $edge, ?array $sourceNode): int
    {
        $handle = $edge['sourceHandle'] ?? null;
        if ($handle === null || $handle === '') {
            return PHP_INT_MAX;
        }
        foreach ($sourceNode['data']['routes'] ?? [] as $route) {
            if (($route['handle'] ?? null) === $handle && isset($route['order'])) {
                return (int) $route['order'];
            }
        }

        return preg_match('/route-(\d+)/', $handle, $m) ? (int) $m[1] : PHP_INT_MAX;
    }

    private static function dfs(array $nodes, array $edges, string $direction): array
    {
        if ($nodes === []) {
            return [];
        }
        $tb = $direction === 'TB';
        $nodeMap = [];
        foreach ($nodes as $node) {
            $nodeMap[(string) $node['id']] = $node;
        }
        $width = self::NODE_WIDTH;
        $height = self::DEFAULT_NODE_HEIGHT;

        $edgesWithSource = array_values(array_filter($edges, fn ($e) => isset($nodeMap[(string) $e['source']], $nodeMap[(string) $e['target']])));
        $sorted = $edgesWithSource;
        usort($sorted, function ($a, $b) use ($nodeMap) {
            if ((string) $a['source'] !== (string) $b['source']) {
                return self::compareIds((string) $a['source'], (string) $b['source']);
            }
            $source = $nodeMap[(string) $a['source']] ?? null;

            return self::edgeOrder($a, $source) <=> self::edgeOrder($b, $source);
        });

        $children = [];
        foreach ($sorted as $edge) {
            $s = (string) $edge['source'];
            $t = (string) $edge['target'];
            $children[$s] ??= [];
            if (! in_array($t, $children[$s], true)) {
                $children[$s][] = $t;
            }
        }

        $targets = [];
        foreach ($edgesWithSource as $edge) {
            $targets[(string) $edge['target']] = true;
        }
        $roots = [];
        foreach ($nodes as $node) {
            if (! isset($targets[(string) $node['id']])) {
                $roots[] = (string) $node['id'];
            }
        }
        if ($roots === []) {
            $roots = array_map(fn ($n) => (string) $n['id'], $nodes);
        }
        $triggerId = null;
        foreach ($nodes as $node) {
            if (($node['data']['microservice'] ?? null) === 'trigger' || ($node['type'] ?? null) === 'flow-trigger') {
                $triggerId = (string) $node['id'];
                break;
            }
        }
        if ($triggerId !== null && in_array($triggerId, $roots, true)) {
            $roots = array_merge([$triggerId], array_values(array_filter($roots, fn ($id) => $id !== $triggerId)));
        }

        // Post-order DFS: children before parents.
        $postOrder = [];
        $visited = [];
        $visit = function (string $id) use (&$visit, &$visited, &$postOrder, $children): void {
            if (isset($visited[$id])) {
                return;
            }
            $visited[$id] = true;
            foreach ($children[$id] ?? [] as $child) {
                $visit($child);
            }
            $postOrder[] = $id;
        };
        foreach ($roots as $root) {
            $visit($root);
        }
        foreach ($nodes as $node) {
            if (! isset($visited[(string) $node['id']])) {
                $postOrder[] = (string) $node['id'];
            }
        }

        // Phase 1: subtree breadth and depth, bottom-up.
        $subtreeWidth = [];
        $subtreeHeight = [];
        foreach ($postOrder as $id) {
            $kids = $children[$id] ?? [];
            if ($kids === []) {
                $subtreeWidth[$id] = $tb ? $width : $height;
                $subtreeHeight[$id] = $tb ? $height : $width;

                continue;
            }
            $childWidths = array_map(fn ($c) => $subtreeWidth[$c] ?? ($tb ? $width : $height), $kids);
            $childHeights = array_map(fn ($c) => $subtreeHeight[$c] ?? ($tb ? $height : $width), $kids);
            $totalBreadth = array_sum($childWidths) + (count($kids) - 1) * ($tb ? self::H_SPACING : self::V_SPACING);
            $subtreeWidth[$id] = max($tb ? $width : $height, $totalBreadth);
            $subtreeHeight[$id] = max($childHeights) + self::V_SPACING + ($tb ? $height : $width);
        }

        // Phase 2: depths by BFS from the roots.
        $depth = [];
        $queue = $roots;
        foreach ($roots as $root) {
            $depth[$root] = 0;
        }
        while ($queue !== []) {
            $id = array_shift($queue);
            $d = $depth[$id] ?? 0;
            foreach ($children[$id] ?? [] as $child) {
                if (! array_key_exists($child, $depth)) {
                    $depth[$child] = $d + 1;
                    $queue[] = $child;
                }
            }
        }
        foreach ($nodes as $node) {
            $depth[(string) $node['id']] ??= 0;
        }

        $rankMaxSize = [];
        foreach ($nodes as $node) {
            $d = $depth[(string) $node['id']];
            $rankMaxSize[$d] = max($rankMaxSize[$d] ?? 0, $tb ? $height : $width);
        }
        foreach ($children as $parentId => $kids) {
            if (count($kids) < 3) {
                continue;
            }
            $microservice = $nodeMap[$parentId]['data']['microservice'] ?? null;
            if ($microservice !== 'route' && $microservice !== 'branch') {
                continue;
            }
            $childDepth = ($depth[$parentId] ?? 0) + 1;
            $rankMaxSize[$childDepth] = ($rankMaxSize[$childDepth] ?? 0) + ((count($kids) - 1) / 2) * self::BRANCH_STAGGER;
        }

        $sortedDepths = array_keys($rankMaxSize);
        sort($sortedDepths);
        $rankStart = [];
        $firstRankSize = $rankMaxSize[0] ?? ($tb ? $height : $width);
        $cumulative = -$firstRankSize / 2;
        foreach ($sortedDepths as $d) {
            $rankStart[$d] = $cumulative;
            $cumulative += ($rankMaxSize[$d] ?? 0) + self::V_SPACING;
        }

        $centerX = [];
        $centerY = [];
        $rootSpacing = $tb ? self::H_SPACING : self::V_SPACING;
        $rootOffset = 0;
        foreach ($roots as $i => $rootId) {
            $breadth = $subtreeWidth[$rootId] ?? ($tb ? $width : $height);
            $start = $rankStart[0] ?? 0;
            $rankSize = $rankMaxSize[0] ?? ($tb ? $height : $width);
            if ($i === 0) {
                $rootOffset = -$breadth / 2;
            }
            if ($tb) {
                $centerX[$rootId] = $rootOffset + $breadth / 2;
                $centerY[$rootId] = $start + $rankSize / 2;
            } else {
                $centerX[$rootId] = $start + $rankSize / 2;
                $centerY[$rootId] = $rootOffset + $breadth / 2;
            }
            $rootOffset += $breadth + $rootSpacing;
        }

        // Children under parents, parents first (BFS).
        $placed = array_fill_keys($roots, true);
        $parentQueue = $roots;
        while ($parentQueue !== []) {
            $parentId = array_shift($parentQueue);
            $kids = $children[$parentId] ?? [];
            if ($kids === []) {
                continue;
            }
            $parentCenterX = $centerX[$parentId] ?? 0;
            $parentCenterY = $centerY[$parentId] ?? 0;
            $childDepth = ($depth[$parentId] ?? 0) + 1;
            $childStart = $rankStart[$childDepth] ?? 0;
            $childRankSize = $rankMaxSize[$childDepth] ?? ($tb ? $height : $width);
            $childBreadths = array_map(fn ($c) => $subtreeWidth[$c] ?? ($tb ? $width : $height), $kids);
            $siblingSpacing = $tb ? self::H_SPACING : self::V_SPACING;
            $totalBreadth = array_sum($childBreadths) + (count($kids) - 1) * $siblingSpacing;
            $offset = -$totalBreadth / 2;
            $microservice = $nodeMap[$parentId]['data']['microservice'] ?? null;
            $stagger = count($kids) >= 3 && ($microservice === 'route' || $microservice === 'branch');
            $centerIndex = (count($kids) - 1) / 2;
            foreach ($kids as $i => $childId) {
                $cb = $childBreadths[$i];
                $maxDistance = (count($kids) - 1) / 2;
                $staggerOffset = $stagger ? ($maxDistance - abs($i - $centerIndex)) * self::BRANCH_STAGGER : 0;
                if ($tb) {
                    $centerX[$childId] = $parentCenterX + $offset + $cb / 2;
                    $centerY[$childId] = $childStart + $childRankSize / 2 + $staggerOffset;
                } else {
                    $centerX[$childId] = $childStart + $childRankSize / 2 + $staggerOffset;
                    $centerY[$childId] = $parentCenterY + $offset + $cb / 2;
                }
                $offset += $cb + $siblingSpacing;
                if (! isset($placed[$childId])) {
                    $placed[$childId] = true;
                    $parentQueue[] = $childId;
                }
            }
        }

        $disconnectedOffset = $rootOffset;
        foreach ($nodes as $node) {
            $id = (string) $node['id'];
            if (array_key_exists($id, $centerX)) {
                continue;
            }
            $breadth = $tb ? $width : $height;
            $start = $rankStart[0] ?? 0;
            $rankSize = $rankMaxSize[0] ?? ($tb ? $height : $width);
            if ($tb) {
                $centerX[$id] = $disconnectedOffset + $breadth / 2;
                $centerY[$id] = $start + $rankSize / 2;
            } else {
                $centerX[$id] = $start + $rankSize / 2;
                $centerY[$id] = $disconnectedOffset + $breadth / 2;
            }
            $disconnectedOffset += $breadth + $rootSpacing;
        }

        $positions = [];
        foreach ($nodes as $node) {
            $id = (string) $node['id'];
            $positions[$id] = [
                'x' => ($centerX[$id] ?? 0) - $width / 2,
                'y' => ($centerY[$id] ?? 0) - $height / 2,
            ];
        }

        return $positions;
    }
}
