# binny

Manage a directory of binaries without a package manager.


![binny-demo](https://github.com/anchore/binny/assets/590471/cdfda64f-0ead-4604-8565-34a397a031b2)


## Installation

```bash
curl -sSfL https://get.anchore.io/binny | sudo sh -s -- -b /usr/local/bin
```

... or, you can specify a release version and destination directory for the installation:

```bash
curl -sSfL https://get.anchore.io/binny | sudo sh -s -- -b <DESTINATION_DIR> <RELEASE_VERSION>
```

## Usage

Keep a configuration in your repo with the binaries you want to manage. For example:

```yaml
# .binny.yaml
tools:
    - name: gh
      version:
        want: v2.33.0
        constraint: < v3
      method: github-release
      with:
        repo: cli/cli
    
    - name: quill
      version:
        want: v0.4.1
        constraint: < v0.5
      method: github-release
      with:
        repo: anchore/quill
    
    - name: chronicle
      version:
        want: v0.7.0
      method: github-release
      with:
        repo: anchore/chronicle
```

Then you can run:
  - `binny install [name...]` to install all tools in the configuration (or the given tool names)
  - `binny check` to verify all configured tools are installed, return exit code 1 if any are missing or inconsistent
  - `binny update [name...]` to update any pinned versions in the configuration with the latest available versions (and within any given constraints)
  - `binny list` to list all tools in the configuration and the installed store

Use `--ignore-cooldown` with `install` or `update` to bypass the release cooldown check.

You can add tools to the configuration one of two ways:
    - manually, by adding a new entry to the configuration file (see the [Configuration](#configuration) section below)
    - with the `binny add <method>` commands, which will handle the configuration for you


## Configuration

The configuration file is a YAML file with a list of tools to manage. Each tool has a name, a version, and
a method for installing it. You can optionally specify a specific method for checking the latest version of
the tool, however, this is not necessary as all install methods have a default version resolver.


### Global Configuration

Top-level options that apply to all tools:

| Option     | Description                                                                                                                                                                     |
|------------|---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `cooldown` | A duration to wait after a version is published before it can be installed (e.g. `7d`, `168h`). This is a supply chain security measure that gives time for malicious versions to be detected and pulled. Individual tools can override this value. Only applies to `install` and `update` commands, and only with `github-release` and `go-proxy` version resolvers (the `git` resolver does not support cooldown). |


```yaml
# .binny.yaml
cooldown: 7d
tools:
    - name: gh
      # ...
```

### Tool Configuration

Each tool has the following configuration options:

```yaml
name: chronicle
version:
  want: v0.7.0
  constraint: <= v0.9.0
  cooldown: 3d  # optional: override the global cooldown for this tool
  method: github-release
  with:
    # arbitrary key-value pairs for the version resolver method
method: go-install
with:
  # arbitrary key-value pairs for the install method
```

| Option         | Description                                                                                                                                               |
|----------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`         | The name of the tool to install. This is used to determine the installation directory and the name of the binary.                                         |
| `version.want` | The version of the tool to install. This can be a specific version, or a version range.                                                                   |
| `version.constraint` | A constraint on the version of the tool to install. This is used to determine the latest version of the tool to update to.                          |
| `version.cooldown` (optional) | A per-tool cooldown duration that overrides the global `cooldown` value (e.g. `3d`, `0` to disable). Only applies when resolving the latest version during `install` or `update`. Not supported by the `git` version resolver. |
| `version.method` | The method to use to determine the latest version of the tool. See the [Version Resolver Methods](#version-resolver-methods) section for more details.  |
| `version.with` | The configuration options for the version method. See the [Version Resolver Methods](#version-resolver-methods) section for more details.                                       |
| `method`       | The method to use to install the tool. See the [Install Methods](#install-methods) section for more details.                                                           |
| `with`        | The configuration options for the install method. See the [Install Methods](#install-methods) section for more details.                                                 |


### Install Methods

Install methods specify where the tool binary should be pulled or built from.


#### `github-release`

The `github-release` install method uses the GitHub Releases API to download the latest release of a tool. It requires the following configuration options:

| Option | Description                                                                                     |
|--------|-------------------------------------------------------------------------------------------------|
| `repo` | The GitHub repository to reference releases from. This should be in the format `<owner>/<repo>` |
| `assets` (optional) | Regex pattern(s) to filter release assets. Can be a single string or array of strings for priority matching |

When multiple assets match the OS/architecture, the `assets` field allows you to specify which one to select:

```yaml
# single regex pattern
- name: hugo
  method: github-release
  with:
    repo: gohugoio/hugo
    assets: "^hugo_extended_[0-9]"  # match hugo_extended but not hugo_extended_withdeploy

# multiple patterns (first match wins)
- name: tool
  method: github-release
  with:
    repo: owner/tool
    assets:
      - "^tool_premium_[0-9]"   # try premium version first
      - "^tool_[0-9]"           # fall back to standard version
```

The default version resolver for this method is `github-release`.


#### `go-install`

The `go-install` install method uses `go install` to install a tool. It requires the following configuration options:

| Option                  | Description                                                                          |
|-------------------------|--------------------------------------------------------------------------------------|
| `module`                | The FQDN to the Go module (e.g. `github.com/anchore/syft`)                           |
| `entrypoint` (optional) | The path within the repo to the main package for the tool (e.g. `cmd/syft`)          |
| `ldflags` (optional)    | A list of ldflags to pass to `go install` (e.g. `-X main.version={{ .Version }}`)    |
| `args` (optional)       | A list of args/flags to pass to `go install` (e.g. `-tags containers_image_openpgp`) |
| `env` (optional)        | A list key=value environment variables to use when running `go install`              |

The `module` option allows for a special entry:
- `.` or `path/to/module/on/disk`

The `ldflags` allow for templating with the following variables:

| Variable | Description                                                                                         |
|--------|-------------------------------------------------------------------------------------------------------|
| `{{ .Version }}` | The resolved version of the tool (which may differe from that of the `version.want` value)  |

In addition to these variables, [sprig functions](http://masterminds.github.io/sprig/) are allowed; for example:
```yaml
ldflags:
- -X main.buildDate={{ now | date "2006-01-02T15:04:05Z07:00" }}
```

For more information about sprig functions, see the [sprig documentation](http://masterminds.github.io/sprig/).

The default version resolver for this method is `go-proxy`.


#### `go-build`

The `go-build` install method builds a tool from source by cloning the repository and running `go build`.
Unlike `go-install`, this method honors `replace` directives in `go.mod` since the build happens within
the full repository context. This is useful for tools that rely on replace directives for local development,
forks, or patches.

| Option                  | Description                                                                                             |
|-------------------------|---------------------------------------------------------------------------------------------------------|
| `module`                | The FQDN to the Go module (e.g. `github.com/anchore/syft`)                                              |
| `entrypoint` (optional) | The path within the repo to the main package for the tool (e.g. `cmd/syft`)                             |
| `ldflags` (optional)    | A list of ldflags to pass to `go build` (e.g. `-X main.version={{ .Version }}`)                         |
| `args` (optional)       | A list of args/flags to pass to `go build` (e.g. `-trimpath`)                                           |
| `env` (optional)        | A list key=value environment variables to use when running `go build`                                   |
| `source` (optional)     | How to obtain source code: `git` (default) clones the repository, `go-proxy` downloads via go mod cache |
| `repo-url` (optional)   | Explicit git repository URL (auto-derived for `github.com` modules)                                     |

The `module` option allows for a special entry:
- `.` or `path/to/module/on/disk`

When using a local path, no cloning or downloading occurs - the build runs directly in the specified directory,
honoring any `replace` directives in the local `go.mod`.

The `ldflags`, `args`, and `env` options support templating with the following variables:

| Variable           | Description                                                                          |
|--------------------|--------------------------------------------------------------------------------------|
| `{{ .Version }}`   | The resolved version of the tool (which may differ from the `version.want` value)   |

In addition to these variables, [sprig functions](http://masterminds.github.io/sprig/) are allowed.

**Source modes:**

- `git` (default): Clones the repository using git. For GitHub modules, the repository URL is automatically
  derived from the module path. For other hosts, use the `repo-url` option to specify the git URL explicitly.

- `go-proxy`: Downloads the source via `go mod download`. This uses the Go module proxy cache and may be
  faster for publicly available modules, but does not support private repositories without proper GOPRIVATE
  configuration.

**Example configurations:**

```yaml
# Build from GitHub source (URL auto-derived)
- name: mytool
  version:
    want: v1.2.3
  method: go-build
  with:
    module: github.com/owner/repo
    entrypoint: cmd/mytool

# Build with custom ldflags
- name: mytool
  version:
    want: v1.0.0
  method: go-build
  with:
    module: github.com/owner/repo
    entrypoint: cmd/mytool
    ldflags:
      - -X main.version={{ .Version }}
      - -X main.commit=abc123
    env:
      - CGO_ENABLED=0

# Build from go proxy source instead of git
- name: mytool
  version:
    want: v1.0.0
  method: go-build
  with:
    module: github.com/owner/repo
    source: goproxy

# Build from non-GitHub host with explicit repo URL
- name: mytool
  version:
    want: v1.0.0
  method: go-build
  with:
    module: gitlab.com/owner/repo
    repo-url: https://gitlab.com/owner/repo.git
```

The default version resolver for local modules is `git`. For GitHub modules, the default is `github-release`.
For other remote modules, the default is `go-proxy`.


#### `hosted-shell`

The `hosted-shell` install method uses a hosted shell script to install a tool. It requires the following configuration options:

| Option | Description                                                                                                |
|--------|------------------------------------------------------------------------------------------------------------|
| `url` | The URL to the hosted shell script (e.g. `https://raw.githubusercontent.com/anchore/syft/main/install.sh`)  |
| `args` (optional) | Arguments to pass to the shell script (as a single string)                                      |

If the URL refers to either `github.com` or `raw.githubusercontent.com` then the default version resolver is `github-release`. 
Otherwise, the version resolver must be specified manually.



### Version Resolver Methods

The version method specifies how to determine the latest version for a tool.

#### `git`

The `git` version method will use a git repo on disk as a source for resolving versions via tags. It requires the following configuration options:

| Option | Description                            |
|--------|----------------------------------------|
| `path` | The path to the git repository on disk |

The `version.want` option allows a special entry:
- `current`: use the current commit checked out in the repo

**note**: this method is still under development. Currently it is most useful for tools that are being used where that are developed:

```yaml
  - name: binny
    version:
      # since the module is . then "current" means whatever is checked out
      want: current
    method: go-install
    with:
      # aka: github.com/anchore/binny, without going through github / go-proxy (stay local)
      module: .
      entrypoint: cmd/binny
      ldflags:
        - -X main.version={{ .Version }}
        - -X main.gitCommit={{ .Version }}
        - -X main.gitDescription={{ .Version }}
        # note: sprig functions are available: http://masterminds.github.io/sprig/
        - -X main.buildDate={{ now | date "2006-01-02T15:04:05Z07:00" }}
```

#### `github-release`

The `github-release` version method uses the GitHub Releases API to determine the latest release of a tool. It requires the following configuration options:

| Option   | Description                                                                                     |
|----------|-------------------------------------------------------------------------------------------------|
| `binary` | Binary to select if there are multiple within the release archive (defaults to the tool name)   |
| `repo`   | The GitHub repository to reference releases from. This should be in the format `<owner>/<repo>` |

The `version.want` option allows a special entry:
- `latest`: don't pin to a version, use the latest available

Note: this approach will require a GitHub API token to be set in the `GITHUB_TOKEN` environment variable if there
is a version constraint or release cooldown used.

#### `go-proxy`

The `go-proxy` version method reaches out to `proxy.golang.org` to determine the latest version of a Go module. It requires the following configuration options:

| Option | Description                                                                                                                              |
|--------|------------------------------------------------------------------------------------------------------------------------------------------|
| `module` | The FQDN to the Go module (e.g. `github.com/anchore/syft`)                                                                             |
| `allow-unresolved-version` | If the latest version cannot be found by the proxy allow for "latest" as a valid value (which `go install` supports) | 

The `version.want` option allows a special entry:
- `latest`: don't pin to a version, use the latest available
