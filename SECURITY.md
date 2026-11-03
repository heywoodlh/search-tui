# Security

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's private vulnerability
reporting (**Security → Report a vulnerability**) rather than in a public
issue. Include a description, the steps to reproduce and the affected version
(`search --version`).

Only the latest release and `main` receive fixes.

## Threat model

`search` runs with your privileges and handles content from untrusted sources:
search engines, websites and code hosts. The main risks and how they are
handled:

| Risk | Mitigation |
| --- | --- |
| **Terminal escape injection.** Result titles, snippets, code, fetched pages and error bodies could contain escape sequences that rewrite the screen, set the clipboard (OSC 52) or change the window title. Bidi overrides ("Trojan Source") could make code read differently from how it runs. | Everything from the network passes through `internal/safe` before it is displayed, copied, saved or summarised. Control characters, C1 codes and bidi controls are removed, and invalid UTF-8 is replaced. Error and status messages are sanitised as they are shown. |
| **Dangerous links.** A result URL could use `file:`, `javascript:`, `smb:` (NTLM leak), `ms-msdt:` or other handler schemes. | Only absolute `http(s)` URLs without control characters are opened (`safe.WebURL`). The URL is passed to the browser as a single argument and never through a shell. |
| **Command injection when opening a browser on Windows.** `cmd /c start` treats `&`, `\|` and `^` in a URL as syntax. | `cmd` is never used. The default is `rundll32 url.dll,FileProtocolHandler`. Name a browser by its path. |
| **Credential leaks on redirects.** Go strips `Authorization` and `Cookie` on cross-host redirects, but not custom auth headers such as `PRIVATE-TOKEN` or `X-Subscription-Token`. | A redirect to another host, or from `https` to `http`, drops every header except a small allowlist. Redirects are capped at 10. |
| **Credentials in error messages.** Some APIs take keys as query parameters (Google `key=`, Mojeek `api_key=`, Kagi `token=`), and Go includes the URL in network errors. | URLs in transport errors are redacted. |
| **Untrusted files opened in `$EDITOR`.** Remote file names could escape the temporary directory or look like options. Vim modelines and Emacs file-local variables can run code. | Names are reduced to a safe base name (`SafeFileName`) in a private `0700` temporary directory, as `0600` files that are removed afterwards. vim and neovim are started with `--cmd "set nomodeline"`, and Emacs with local variables disabled. |
| **Secrets in readable config files.** | Prefer `*_file`, `*_command` or `*_env`. A warning is shown when the config contains literal credentials and is readable by other users, for example a home-manager file in the Nix store. |
| **Stalled servers.** | Every HTTP request has a timeout and a response-size cap (`max_body_kb`). Inline searches have an overall deadline, including streaming engines. |
| **Vulnerable dependencies or toolchain.** | `govulncheck` runs in CI. Go is pinned with a `toolchain` directive, and Dependabot opens weekly update PRs. |

Trusted inputs are your own config file and environment. Commands in
`*_command`, `summary.command`, `clipboard_command`, `browser` and `editor`
are run because you configured them.

## CI security posture

Every pull request must pass the **`posture`** check in the `CI` workflow,
which requires all of the following:

- **Tests:** `go vet` and `go test` on Linux (with the race detector), macOS
  and Windows, plus cross-compiles.
- **Nix:** `nix flake check` (package build, tests, home-manager module).
- **golangci-lint** (`.golangci.yml`): gofmt, govet, staticcheck, errcheck,
  gosec, bidichk, bodyclose, noctx, errorlint and more. Every `//nolint` must
  name the linter and give a reason.
- **govulncheck:** no reachable code with known vulnerabilities.
- **actionlint** and **zizmor:** workflow correctness and security. Actions
  are pinned to commit SHAs, use least-privilege permissions per job, never
  persist credentials, and avoid template injection and cache poisoning.
- **shellcheck** for all scripts.
- **gitleaks:** no secrets anywhere in git history.
- **nixfmt** for Nix files.

Run the same gates locally:

```sh
nix develop .#ci -c scripts/check.sh        # or: scripts/check.sh go vuln ...
```

Workflow hardening applies to the other workflows too. The Pages workflow
builds the site, including running the PR's code to record the demo, with a
read-only token. Only separate jobs that never execute PR code can push to
`gh-pages` or comment on PRs. The release workflow never restores a cache.

### Enforcing the gate

A repository admin enables enforcement once. This ruleset requires a
squash-merged pull request with passing `posture` and `semver` checks (see
"Versioning and releases" in the README) before anything reaches `main`, and
blocks force-pushes and deletion:

```sh
gh api -X POST repos/heywoodlh/tui-search/rulesets --input - <<'JSON'
{
  "name": "main: require PR + posture",
  "target": "branch",
  "enforcement": "active",
  "conditions": { "ref_name": { "include": ["~DEFAULT_BRANCH"], "exclude": [] } },
  "rules": [
    { "type": "deletion" },
    { "type": "non_fast_forward" },
    { "type": "pull_request", "parameters": {
        "allowed_merge_methods": ["squash"],
        "required_approving_review_count": 0,
        "dismiss_stale_reviews_on_push": true,
        "require_code_owner_review": false,
        "require_last_push_approval": false,
        "required_review_thread_resolution": false } },
    { "type": "required_status_checks", "parameters": {
        "strict_required_status_checks_policy": true,
        "required_status_checks": [
          { "context": "posture", "integration_id": 15368 },
          { "context": "semver", "integration_id": 15368 } ] } }
  ]
}
JSON
```

Also turn on **Settings → Code security → Private vulnerability reporting**
and **Dependabot alerts**.
