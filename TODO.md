# TODO

- Move the Go repos still declaring `github.com/charles-haynes/...` module paths to `github.com/cehbz/...`. Mechanical (path rewrite, build, test, one commit each): html2bbcode, check_space (`/v2`), strsim, ratelimiter. Each needs a decision first: docconv (fork, 24 references; fix or archive), whatdb (imports archived whatapi; unarchive whatapi or leave the redirect), gazelle (doesn't build: `replace` to sibling `../discogs` and `../whatapi`, and the old discogs library is gone; archive or recover).
- Evidence layer: read Gmail through our own REST client for Grab receipts, travel confirmations and Wise transfer recipients. A Wise debit on Checking is sometimes a top-up of a THB balance that later funds several transfers, so it has no single recipient.
- Automatic runs: schedule `categorize run`, retrying the PocketSmith timeouts seen in acceptance (30 s with no response headers, on a PUT and on `GET /me`).
- Grab and Patreon can include widely varying amounts under a single payee, which can trigger `attention-large`; consider adding more context to disambiguate.
- Unattended secrets through a 1Password service account: a vault holding only the categorizer's secrets, the token in the Keychain, `op` reading without a prompt. Owner-only files hold them now. Plan eligibility for service accounts is unverified.
- Rename the Big category and its subcategories to Trips, with the config's `[trips.big]` table, so nothing reads `trips.trips`.
