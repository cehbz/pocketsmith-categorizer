# TODO

- Move the Go repos still declaring `github.com/charles-haynes/...` module paths to `github.com/cehbz/...`. Mechanical (path rewrite, build, test, one commit each): html2bbcode, check_space (`/v2`), strsim, ratelimiter. Each needs a decision first: docconv (fork, 24 references; fix or archive), whatdb (imports archived whatapi; unarchive whatapi or leave the redirect), gazelle (doesn't build: `replace` to sibling `../discogs` and `../whatapi`, and the old discogs library is gone; archive or recover).
