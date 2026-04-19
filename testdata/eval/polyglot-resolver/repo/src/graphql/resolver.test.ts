// Tests for resolver.ts — paired by §7.3.3's JS/TS test-linking rule.
// The committed file makes the `missing_tests` gap NOT fire in this
// fixture; a separate `tests/resolver.no-test.ts` demonstrates the
// inverse case.

import { RateLimitResolver, applyRateLimitHeader } from "./resolver";

describe("RateLimitResolver", () => {
  it("resolves the limit from context", () => {
    const r = new RateLimitResolver();
    expect(r.resolveRateLimit({ userId: "u", limit: 7 })).toBe(7);
  });
  it("formats the header", () => {
    expect(applyRateLimitHeader(100)).toContain("X-RateLimit-Remaining");
  });
});
