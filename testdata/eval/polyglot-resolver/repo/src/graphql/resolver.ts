// GraphQL resolver — the real answer to the fixture's task.
// A v1.1 tier-2 parse picks up `Resolver`, `resolveRateLimit`, and
// the `RATE_LIMIT_HEADER` constant, giving this file the s_symbol
// score that matches anchors like "RateLimit" / "header".

import { Context } from "./context";

export const RATE_LIMIT_HEADER = "X-RateLimit-Remaining";

export interface Resolver {
  resolveRateLimit(ctx: Context): number;
}

export class RateLimitResolver implements Resolver {
  resolveRateLimit(ctx: Context): number {
    return ctx.limit;
  }
}

export function applyRateLimitHeader(n: number): string {
  return `${RATE_LIMIT_HEADER}: ${n}`;
}
