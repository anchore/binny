# binny

Manage a directory of binaries without a package manager.

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
- name: gh
  version:
    want: v2.33.0
  method: github-release
  with:
    repo: cli/cli

- name: quill
  version:
    want: v0.4.1
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
  - `benny install` to install all binaries in the configuration
  - `benny install <name>` to install a specific binary
  - `benny check` to verify all configured binaries are installed
  - `benny update-lock` to update any pinned versions in the configuration with the latest available versions
