"""Python side of the billing service. Tier-2 (tree_sitter_python)
exposes refund_charge and RefundReport as module-level symbols."""


class RefundReport:
    """Captures refund outcomes for downstream reconciliation."""

    def __init__(self, charge_id: str):
        self.charge_id = charge_id


def refund_charge(charge_id: str, amount: int) -> RefundReport:
    return RefundReport(charge_id)
