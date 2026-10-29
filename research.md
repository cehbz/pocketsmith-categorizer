# Research

## Exchange-rate source

Fit against three Grab statements of two accounts (May–Oct 2026; 509 trips, 508 with a nonzero total) and 1,007 card charges: greedy matching, 5% cap, posting lag 0–3 days in the ledger time zone (US Eastern), rate taken on the payment's ledger date (latest rate on or before it).

| Source | Matched | Median abs error | p95 | Signed median | Within 1% | Within 2% |
|---|---|---|---|---|---|---|
| ECB via Frankfurter | 478 | 0.36% | 1.06% | +0.33% | 451 | 470 |
| Yahoo `THB=X` chart | 481 | 0.31% | 0.97% | +0.29% | 457 | 476 |
| Wise rate history | 480 | 0.34% | 0.95% | +0.31% | 459 | 474 |

- The sources fit equally well. Charges run about 0.3% above the reference rate.
- Frankfurter is documented and keyless: `https://api.frankfurter.dev/v1/{start}..{end}?from=USD&to=THB`, business days only (2026-04-24 is followed by 2026-04-27). Yahoo's chart endpoint is unofficial. Wise's `wise.com/rates/history+live` is undocumented and returns 400 for `unit=day` with `length` 90 or more; `unit=month&length=7` works.
- A 2% tolerance keeps 470 of the 478 pairs found under the 5% cap.

## Posting window

ECB rates, 2% tolerance, greedy matching:

| Ledger date zone | Lag window | Matched | At lag 0 |
|---|---|---|---|
| America/New_York | 0..3 | 470 | 389 |
| America/New_York | -1..3 | 470 | 381 |
| America/Los_Angeles | 0..3 | 476 | 237 |
| UTC | 0..3 | 423 | 340 |

- The ledger zone with a 0..3 day lag concentrates pairs at lag 0. Allowing -1, or shifting zones west, swaps pairs among small rides with the same fare rather than matching more payments.

## Statements

- Two statements of one account overlap on 2026-06-11 and share 2 bookings.
- One trip has a ฿0 total (a promo ride) and puts no charge on the card.
- 14 of 509 trips print the service label off the amount line ("Standard" above it, "(JustGrab)" below).
- 64 trips wrap the booking ID: "A-" on the trip's first line, the rest on a later line.
