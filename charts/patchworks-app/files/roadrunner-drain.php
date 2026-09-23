<?php

sleep((int) $argv[1]);

$timeoutSeconds = (int) $argv[2];
$stateFile = $argv[3];

if (! is_file($stateFile)) {
    exit(0);
}

$state = json_decode(file_get_contents($stateFile), true);
$pid = $state['masterProcessId'] ?? null;

if (! is_int($pid) || $pid <= 1) {
    fwrite(STDERR, "RoadRunner drain: invalid master process ID.\n");
    exit(1);
}

if (! posix_kill($pid, 0)) {
    exit(0);
}

if (! posix_kill($pid, SIGTERM)) {
    fwrite(STDERR, "RoadRunner drain: failed to signal the master process.\n");
    exit(1);
}

$deadline = hrtime(true) + $timeoutSeconds * 1_000_000_000;

while (posix_kill($pid, 0)) {
    if (hrtime(true) >= $deadline) {
        fwrite(STDERR, "RoadRunner drain: timed out waiting for the master process.\n");
        exit(1);
    }

    usleep(100_000);
}
