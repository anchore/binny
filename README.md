# binny

Manage a directory of binaries without a package manager.


![binny-demo](https://github.com/anchore/binny/assets/590471/cdfda64f-0ead-4604-8565-34a397a031b2)


## Installation

```bash
curl -sSfL https://raw.githubusercontent.com/anchore/binny/main/install.sh | sh -s -- -b /usr/local/bin
```

... or, you can specify a release version and destination directory for the installation:

```bash
curl -sSfL https://raw.githubusercontent.com/anchore/binny/main/install.sh | sh -s -- -b <DESTINATION_DIR> <RELEASE_VERSION>
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

You can add tools to the configuration one of two ways:
    - manually, by adding a new entry to the configuration file (see the [Configuration](#configuration) section below)
    - with the `binny add <method>` commands, which will handle the configuration for you


## Configuration

The configuration file is a YAML file with a list of tools to manage. Each tool has a name, a version, and 
a method for installing it. You can optionally specify a specific method for checking the latest version of 
the tool, however, this is not necessary as all install methods have a default version resolver.


### Tool Configuration

Each tool has the following configuration options:

```yaml
name: chronicle
version:
  want: v0.7.0
  constraint: <= v0.9.0
  method: github-release
  with:
    # arbitrary key-value pairs for the version resolver method
method: go-install
with:
  # arbitrary key-value pairs for the install method
```

| Option         | Description                                                                                                                                            |
|----------------|--------------------------------------------------------------------------------------------------------------------------------------------------------|
| `name`         | The name of the tool to install. This is used to determine the installation directory and the name of the binary.                                      |
| `version.want` | The version of the tool to install. This can be a specific version, or a version range.                                                                |
| `version.constraint` | A constraint on the version of the tool to install. This is used to determine the latest version of the tool to update to.                             |
| `version.method` | The method to use to determine the latest version of the tool. See the [Version Resolver Methods](#version-resolver-methods) section for more details. |
| `version.with` | The configuration options for the version method. See the [Version Resolver Methods](#version-resolver-methods) section for more details.                       |
| `method`       | The method to use to install the tool. See the [Install Methods](#install-methods) section for more details.                                           |
| `with`        | The configuration options for the install method. See the [Install Methods](#install-methods) section for more details.                                |


### Install Methods

Install methods specify where the tool binary should be pulled or built from.


#### `github-release`

The `github-release` install method uses the GitHub Releases API to download the latest release of a tool. It requires the following configuration options:

| Option | Description                                                                                     |
|--------|-------------------------------------------------------------------------------------------------|
| `repo` | The GitHub repository to reference releases from. This should be in the format `<owner>/<repo>` |

The default version resolver for this method is `github-release`.


#### `go-install`

The `go-install` install method uses `go install` to install a tool. It requires the following configuration options:

| Option                  | Description                                                                 |
|-------------------------|-----------------------------------------------------------------------------|
| `module`           | The FQDN to the Go module (e.g. `github.com/anchore/syft`)                  |
| `entrypoint` (optional) | The path within the repo to the main package for the tool (e.g. `cmd/syft`) |
| `ldflags` (optional)    | A list of ldflags to pass to `go install` (e.g. `-X main.version={{ .Version }}`)                    |

The `module` option allows for a special entry:
- `.` or `path/to/module/on/disk`

The `ldflags` allow for templating with the following variables:

| Variable | Description                                                                                |
|--------|--------------------------------------------------------------------------------------------|
| `{{ .Version }}` | The resolved version of the tool (which may differe from that of the `version.want` value) |

In addition to these variables, [sprig functions](http://masterminds.github.io/sprig/) are allowed; for example:
```yaml
ldflags:
- -X main.buildDate={{ now | date "2006-01-02T15:04:05Z07:00" }}
```

For more information about sprig functions, see the [sprig documentation](http://masterminds.github.io/sprig/).

The default version resolver for this method is `go-proxy`.


#### `hosted-shell`

The `hosted-shell` install method uses a hosted shell script to install a tool. It requires the following configuration options:

| Option | Description                                                |
|--------|------------------------------------------------------------|
| `url` | The URL to the hosted shell script (e.g. `https://raw.githubusercontent.com/anchore/syft/main/install.sh`)                 |
| `args` (optional) | Arguments to pass to the shell script (as a single string) |

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
      # aka: github.com/anchore/binny, without going through github / goproxy (stay local)
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

| Option | Description                                                                                     |
|--------|-------------------------------------------------------------------------------------------------|
| `repo` | The GitHub repository to reference releases from. This should be in the format `<owner>/<repo>` |

The `version.want` option allows a special entry:
- `latest`: don't pin to a version, use the latest available

#### `go-proxy`

The `go-proxy` version method reaches out to `proxy.golang.org` to determine the latest version of a Go module. It requires the following configuration options:

| Option | Description                                                                                      |
|--------|--------------------------------------------------------------------------------------------------|
| `module` | The FQDN to the Go module (e.g. `github.com/anchore/syft`)                  |

The `version.want` option allows a special entry:
- `latest`: don't pin to a version, use the latest available
