# npm installer releases

`digcrawl-skill` is a small installer, separate from the Go program. Its
`package.json` `digcrawlRelease` field pins an existing public GitHub Release.
Never move an existing Go tag to publish an installer update.

1. Work on public `main` only. Do not merge or publish `local/*` branches.
2. Set the npm package version and `digcrawlRelease`, then refresh the lockfile.
   Confirm all five binary assets and `checksums.txt` exist for that release
   and can be downloaded anonymously. A private GitHub repository returns 404
   to users of the public npm installer; obtain the owner's approval before
   changing repository visibility.
3. Run `npm ci --ignore-scripts`, `npm test`, and `npm audit --omit=dev`.
   The tests inspect the actual npm tarball allowlist. No Go source, local
   profiles, cookies, private branches, or runtime data belong in the package.
4. Confirm the exact main commit passes the Linux, macOS, and Windows Test jobs.
5. From that clean main checkout, run `npm publish --access public` after
   authenticating with the npm account that owns `digcrawl-skill`. Complete
   npm's interactive second-factor approval if requested. Do not store tokens
   in the repository or share them in chat.
6. Verify `npm view digcrawl-skill version` and test the published npx command
   with an isolated home and install directory before announcing availability.

There are no npm lifecycle install scripts. Program downloads occur only when
the user explicitly runs `install`; each archive is checked against the
release's SHA-256 manifest before extraction. The npm package `files` allowlist
contains only the CLI, its implementation, and the two READMEs (plus npm's
automatic package metadata and license).
