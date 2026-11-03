package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mitchellh/hashstructure/v2"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/binny/tool/git"
	"github.com/anchore/binny/tool/githubrelease"
	"github.com/anchore/binny/tool/gobuild"
	"github.com/anchore/binny/tool/goinstall"
	"github.com/anchore/binny/tool/goproxy"
	"github.com/anchore/binny/tool/hostedshell"
)

var _ binny.Tool = (*compositeTool)(nil)

type compositeTool struct {
	config Config
	binny.Installer
	binny.VersionResolver
}

type Config struct {
	Name                  string
	InstallerConfig       DetailConfig
	VersionResolverConfig DetailConfig
}

type DetailConfig struct {
	Method     string
	Parameters any
}

func (t *Config) normalize() error {
	// set the version resolution parameters
	if t.VersionResolverConfig.Method == "" {
		resolveMethod, versionParams, err := defaultVersionResolverConfig(t.InstallerConfig.Method, t.InstallerConfig.Parameters)
		if err != nil {
			return fmt.Errorf("failed to get default version resolution method: %w", err)
		}
		t.VersionResolverConfig.Method = resolveMethod
		t.VersionResolverConfig.Parameters = versionParams
	}
	return nil
}

func New(t Config) (binny.Tool, error) {
	if err := t.normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize tool config: %w", err)
	}

	log.WithFields("tool", t.Name, "install-method", t.InstallerConfig.Method, "version-resolver", t.VersionResolverConfig.Method).
		Trace("configuring tool")

	installer, err := getInstaller(t.InstallerConfig.Method, t.InstallerConfig.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to get installer for tool %q: %w", t.Name, err)
	}

	resolver, err := getResolver(t.VersionResolverConfig.Method, t.VersionResolverConfig.Parameters, t.InstallerConfig.Parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to get version resolver for tool %q: %w", t.Name, err)
	}

	if params, ok := t.InstallerConfig.Parameters.(gobuild.InstallerParameters); ok {
		proxyParams, _ := t.VersionResolverConfig.Parameters.(goproxy.VersionResolutionParameters)
		resolver = gobuild.WithRefResolution(params, resolver, proxyParams.AllowUnresolvedVersion)
	}

	return &compositeTool{
		config:          t,
		Installer:       installer,
		VersionResolver: resolver,
	}, nil
}

func getInstaller(method string, installParams any) (installer binny.Installer, err error) {
	switch {
	case goinstall.IsInstallMethod(method):
		params, ok := installParams.(goinstall.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid go install parameters")
		}

		installer = goinstall.NewInstaller(params)
	case gobuild.IsInstallMethod(method):
		params, ok := installParams.(gobuild.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid go-build parameters")
		}

		installer = gobuild.NewInstaller(params)
	case hostedshell.IsInstallMethod(method):
		params, ok := installParams.(hostedshell.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid hosted shell install parameters")
		}

		installer = hostedshell.NewInstaller(params)
	case githubrelease.IsInstallMethod(method):
		params, ok := installParams.(githubrelease.InstallerParameters)
		if !ok {
			return nil, fmt.Errorf("invalid github release install parameters")
		}

		installer = githubrelease.NewInstaller(params)
	}

	if err != nil {
		return nil, err
	}

	return installer, nil
}

func getResolver(method string, params, installParams any) (resolver binny.VersionResolver, err error) {
	switch {
	case goproxy.IsResolveMethod(method):
		config, ok := params.(goproxy.VersionResolutionParameters)
		if !ok {
			return nil, fmt.Errorf("invalid go proxy version resolution parameters")
		}
		config.Env = goCommandEnv(installParams)
		resolver = goproxy.NewVersionResolver(config)
	case githubrelease.IsResolveMethod(method):
		config, ok := params.(githubrelease.VersionResolutionParameters)
		if !ok {
			return nil, fmt.Errorf("invalid github release version resolution parameters")
		}
		resolver = githubrelease.NewVersionResolver(config)
	case git.IsResolveMethod(method):
		config, ok := params.(git.VersionResolutionParameters)
		if !ok {
			return nil, fmt.Errorf("invalid git version resolution parameters")
		}
		resolver = git.NewVersionResolver(config)
	}

	if err != nil {
		return nil, err
	}

	return resolver, nil
}

func (c compositeTool) Name() string {
	return c.config.Name
}

func (c compositeTool) KeepArchiveFiles() bool {
	k, ok := c.Installer.(binny.ArchiveFileKeeper)
	return ok && k.KeepArchiveFiles()
}

// cacheable is false for a tool built from a local module (module: .), which resolves to the checkout's commit, so the
// name and resolved version do not pin the installed bytes.
func (c compositeTool) cacheable() bool {
	switch p := c.config.InstallerConfig.Parameters.(type) {
	case goinstall.InstallerParameters:
		return !gobuild.IsLocalModule(p.Module)
	case gobuild.InstallerParameters:
		return !gobuild.IsLocalModule(p.Module)
	}
	return true
}

// passesThroughUnresolved is true when the resolver can hand back the wanted version as-is (allow-unresolved-version,
// or the git resolver's branch fallback), so what it returns may be a branch that only looks exact, like deadbee or
// release/v1.2.3.
func (c compositeTool) passesThroughUnresolved() bool {
	if git.IsResolveMethod(c.config.VersionResolverConfig.Method) {
		return true
	}
	p, ok := c.config.VersionResolverConfig.Parameters.(goproxy.VersionResolutionParameters)
	return ok && p.AllowUnresolvedVersion
}

// goEnvVars are the go env vars that change what go-install and go-build produce. An unknown one (GOFIPS140 before
// go1.24) prints as empty rather than failing.
//
// ponytail: CC is a name, so a compiler upgrade under the same name keeps the key. Probe `$CC --version` if cgo tools
// turn out to need it.
var goEnvVars = []string{
	"GOVERSION", "CGO_ENABLED", "GOFLAGS", "GOEXPERIMENT", "GOFIPS140",
	"GOAMD64", "GOARM", "GOARM64", "GO386", "GOPPC64", "GOMIPS", "GOMIPS64", "GORISCV64", "GOWASM",
	"CC", "CXX", "CGO_CFLAGS", "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_LDFLAGS",
}

// goEnvs remembers readGoEnv per tool env for the rest of the run.
var goEnvs sync.Map

// goEnv is the go env that decides the bytes a go tool produces, as JSON, or "" when it can't be read. It is read with
// the tool's own env on top of the process env, as the build runs, so a tool that sets GOTOOLCHAIN or GOFLAGS in env:
// is keyed by what it builds with.
var goEnv = func(env []string) string {
	key := strings.Join(env, "\x00")
	if v, ok := goEnvs.Load(key); ok {
		return v.(string)
	}
	v := readGoEnv(env)
	goEnvs.Store(key, v)
	return v
}

func readGoEnv(env []string) string {
	out, err := internal.GoCommand(context.Background(), env, append([]string{"env", "-json"}, goEnvVars...)...)
	if err != nil {
		log.WithFields("error", err).Warn("unable to read the go env for the cache fingerprint, the cache is not used for go tools")
		return ""
	}
	return strings.TrimSpace(string(out))
}

// Fingerprint is the install method and its parameters (before templating) and, for go-install and go-build, the go
// env, as JSON. Install hashes it together with the platform and keep-archive-files into the cache key. It is empty
// when the tool is not cacheable, and for a go tool when the go env can't be read.
func (c compositeTool) Fingerprint() string {
	if !c.cacheable() {
		return ""
	}
	params := c.config.InstallerConfig.Parameters
	var env string
	switch params.(type) {
	case goinstall.InstallerParameters, gobuild.InstallerParameters:
		if env = goEnv(goCommandEnv(params)); env == "" {
			return ""
		}
	}
	// lists stay in order, since args and asset patterns are order sensitive
	b, err := json.Marshal(struct {
		Installer DetailConfig
		GoEnv     string `json:",omitempty"`
	}{c.config.InstallerConfig, env})
	if err != nil {
		log.WithFields("tool", c.Name(), "error", err).Debug("unable to encode the install config, the cache is not used for it")
		return ""
	}
	return string(b)
}

func (c compositeTool) ID() string {
	f, err := hashstructure.Hash(c.config, hashstructure.FormatV2, &hashstructure.HashOptions{
		ZeroNil:      true,
		SlicesAsSets: true,
	})
	if err != nil {
		panic(fmt.Sprintf("could not hash tool config: %+v", err))
	}

	return fmt.Sprintf("%016x", f)
}

func defaultVersionResolverConfig(installMethod string, installParams any) (method string, parameters any, err error) {
	switch {
	case goinstall.IsInstallMethod(installMethod):
		return goinstall.DefaultVersionResolverConfig(installParams)
	case gobuild.IsInstallMethod(installMethod):
		return gobuild.DefaultVersionResolverConfig(installParams)
	case hostedshell.IsInstallMethod(installMethod):
		return hostedshell.DefaultVersionResolverConfig(installParams)
	case githubrelease.IsInstallMethod(installMethod):
		return githubrelease.DefaultVersionResolverConfig(installParams)
	}

	return "", nil, nil
}

// goCommandEnv returns the env a go installer runs the go command with, so version lookups see the same
// GOPROXY/GOPRIVATE/etc as the install. Entries templated on the version are left out, since the version is what
// is being resolved.
func goCommandEnv(installParams any) []string {
	var env []string
	switch p := installParams.(type) {
	case goinstall.InstallerParameters:
		env = p.Env
	case gobuild.InstallerParameters:
		env = p.Env
	}

	return internal.StaticEnv(env)
}
