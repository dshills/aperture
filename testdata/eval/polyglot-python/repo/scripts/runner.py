"""Runner is the Python batch-job driver referenced by the fixture task.

A tier-2 (tree_sitter_python) parse exposes BatchRunner, run_batch, and
process_records as module-level symbols; tier-1-only scoring would miss
these and pick the Go Orchestrator.
"""

import os


class BatchRunner:
    """BatchRunner owns the per-shard processing loop."""

    def __init__(self, shard: int):
        self.shard = shard

    def run_batch(self, records):
        return process_records(records, self.shard)


def process_records(records, shard):
    return [(r, shard) for r in records]


_INTERNAL_CONST = os.environ.get("BATCH_MODE", "default")
