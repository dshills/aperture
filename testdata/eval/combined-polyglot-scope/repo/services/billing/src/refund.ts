// TypeScript side of the billing service. The fixture task
// mentions refundCharge + RefundContext; tier-2 TS extraction
// surfaces them as module-level symbols.

export interface RefundContext {
  chargeId: string;
  amount: number;
}

export function refundCharge(ctx: RefundContext): Promise<void> {
  return Promise.resolve();
}

export class RefundDispatcher {
  dispatch(ctx: RefundContext) {
    return refundCharge(ctx);
  }
}
