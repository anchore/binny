package githubrelease

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/mholt/archiver/v3"
	"github.com/scylladb/go-set/strset"
	"github.com/shurcooL/githubv4"
	"golang.org/x/net/html"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
	"github.com/anchore/go-logger"
)

const checksumsFilename = "checksums.txt"

var archiveMimeTypes = strset.New(
	// archive only
	"application/x-archive",
	"application/x-cpio",
	"application/x-shar",
	"application/x-iso9660-image",
	"application/x-sbx",
	"application/x-tar",
	// compression only
	"application/x-bzip2",
	"application/gzip",
	"application/x-lzip",
	"application/x-lzma",
	"application/x-lzop",
	"application/x-snappy-framed",
	"application/x-xz",
	"application/x-compress",
	"application/zstd",
	// archiving and compression
	"application/x-7z-compressed",
	"application/x-ace-compressed",
	"application/x-astrotite-afa",
	"application/x-alz-compressed",
	"application/vnd.android.package-archive",
	"application/x-freearc",
	"application/x-arj",
	"application/x-b1",
	"application/vnd.ms-cab-compressed",
	"application/x-cfs-compressed",
	"application/x-dar",
	"application/x-dgc-compressed",
	"application/x-apple-diskimage",
	"application/x-gca-compressed",
	"application/java-archive",
	"application/x-lzh",
	"application/x-lzx",
	"application/x-rar-compressed",
	"application/x-stuffit",
	"application/x-stuffitx",
	"application/x-gtar",
	"application/x-ms-wim",
	"application/x-xar",
	"application/zip",
	"application/x-zoo",
)

var binaryMimeTypes = strset.New(
	"application/octet-stream",
	"application/x-executable",
	"application/x-mach-binary",
	"application/x-elf",
	"application/x-sharedlib",
	"application/vnd.microsoft.portable-executable",
	"application/x-executable",
)

var _ binny.Installer = (*Installer)(nil)

type InstallerParameters struct {
	Binary string `json:"binary" yaml:"binary" mapstructure:"binary"`
	Repo   string `json:"repo" yaml:"repo" mapstructure:"repo"`
	Assets any    `json:"assets" yaml:"assets" mapstructure:"assets"`
}

type Installer struct {
	config         InstallerParameters
	assetPatterns  []*regexp.Regexp
	releaseFetcher func(lgr logger.Logger, user, repo, tag string) (*ghRelease, error)
}

func NewInstaller(cfg InstallerParameters) Installer {
	patterns := compileAssetPatterns(cfg.Assets)
	return Installer{
		config:         cfg,
		assetPatterns:  patterns,
		releaseFetcher: fetchRelease,
	}
}

// compileAssetPatterns converts the assets configuration into compiled regex patterns
func compileAssetPatterns(assets any) []*regexp.Regexp {
	if assets == nil {
		return nil
	}

	var patterns []string
	switch v := assets.(type) {
	case string:
		if v != "" {
			patterns = append(patterns, v)
		}
	case []string:
		patterns = v
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				patterns = append(patterns, str)
			}
		}
	default:
		// unsupported type, return nil to indicate no filtering
		return nil
	}

	var compiled []*regexp.Regexp
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			compiled = append(compiled, re)
		}
		// note: silently ignore invalid regex patterns
	}

	return compiled
}

func (i Installer) InstallTo(version, destDir string) (string, error) {
	lgr := log.Nested("tool", fmt.Sprintf("%s@%s", i.config.Repo, version))

	lgr.Debug("installing from github release assets")

	fields := strings.Split(i.config.Repo, "/")
	if len(fields) != 2 {
		return "", fmt.Errorf("invalid github repo format: %q", i.config.Repo)
	}
	user, repo := fields[0], fields[1]

	release, err := i.releaseFetcher(lgr, user, repo, version)
	if err != nil {
		return "", fmt.Errorf("unable to fetch github release %s@%s: %w", i.config.Repo, version, err)
	}

	asset := selectBinaryAsset(lgr, release.Assets, runtime.GOOS, runtime.GOARCH, i.assetPatterns)
	if asset == nil {
		return "", fmt.Errorf("unable to find matching asset for %s@%s", i.config.Repo, version)
	}

	checksumAsset := selectChecksumAsset(lgr, release.Assets)

	binPath, err := downloadAndExtractAsset(lgr, *asset, checksumAsset, destDir, i.config.Binary)
	if err != nil {
		return "", fmt.Errorf("unable to download and extract asset %s@%s: %w", i.config.Repo, version, err)
	}

	return binPath, nil
}

func downloadAndExtractAsset(lgr logger.Logger, asset ghAsset, checksumAsset *ghAsset, destDir string, binary string) (string, error) {
	assetPath := filepath.Join(destDir, asset.Name)

	checksum := asset.Checksum
	if checksumAsset != nil && checksum == "" {
		lgr.WithFields("asset", checksumAsset.Name).Trace("downloading checksum manifest")

		checksumsPath := filepath.Join(destDir, checksumsFilename)

		if err := internal.DownloadFile(lgr, checksumAsset.URL, checksumsPath, ""); err != nil {
			return "", fmt.Errorf("unable to download checksum asset %q: %w", checksumAsset.Name, err)
		}

		var err error
		checksum, err = getChecksumForAsset(asset.Name, checksumsPath)
		if err != nil {
			return "", fmt.Errorf("unable to get checksum for asset %q: %w", asset.Name, err)
		}
	}

	logFields := logger.Fields{
		"destination": assetPath,
	}

	if checksum != "" {
		logFields["checksum"] = checksum
	}

	lgr.WithFields(logFields).Trace("downloading asset")

	if err := internal.DownloadFile(lgr, asset.URL, assetPath, checksum); err != nil {
		return "", fmt.Errorf("unable to download asset %q: %w", asset.Name, err)
	}

	// check if it exists
	v, err := os.Stat(assetPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("asset %q does not exist", assetPath)
	}

	lgr.WithFields("size", v.Size(), "asset", asset.Name).Trace("downloaded asset")

	switch {
	case isArchiveAsset(asset):
		lgr.WithFields("asset", asset.Name).Trace("asset is an archive")
		return extractArchive(assetPath, destDir, binary)
	case isBinaryAsset(asset):
		lgr.WithFields("asset", asset.Name).Trace("asset could be a binary")
		return assetPath, nil
	}

	return "", fmt.Errorf("unsupported asset content-type: %q", asset.ContentType)
}

func isArchiveAsset(asset ghAsset) bool {
	if archiveMimeTypes.Has(asset.ContentType) {
		return true
	}
	return asset.ContentType == "" && hasArchiveExtension(asset.Name)
}

func isBinaryAsset(asset ghAsset) bool {
	if binaryMimeTypes.Has(asset.ContentType) {
		return true
	}
	return asset.ContentType == "" && (hasBinaryExtension(asset.Name))
}

func hasArchiveExtension(name string) bool {
	ext := filepath.Ext(name)
	switch ext {
	// note: we only need to check for the last part of any archive extension (that is, only ".gz" not ".tar.gz")
	case ".tar", ".zip", ".gz", ".bz2", ".xz", ".rar", ".7z", ".tgz", ".bz", ".tbz", ".zst", ".zstd":
		return true
	}
	return false
}

func getChecksumForAsset(assetName, checksumsPath string) (string, error) {
	fh, err := os.Open(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("unable to open checksums file %q: %w", checksumsPath, err)
	}
	defer fh.Close()

	scanner := bufio.NewScanner(fh)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return "", fmt.Errorf("invalid checksum line: %q", line)
		}
		if fields[1] == assetName {
			return fields[0], nil
		}
	}
	return "", nil
}

func extractArchive(archivePath, destDir, binary string) (string, error) {
	// extract tar.gz to destDir
	if err := archiver.Unarchive(archivePath, destDir); err != nil {
		return "", fmt.Errorf("unable to extract asset %q: %w", archivePath, err)
	}

	if err := os.Remove(archivePath); err != nil {
		return "", fmt.Errorf("unable to remove asset archive %q: %w", archivePath, err)
	}

	// look for the binary recursively in the destDir and return that
	binPath, err := findBinaryAssetInDir(binary, destDir)
	if err != nil {
		return "", fmt.Errorf("unable to find binary in %q: %w", destDir, err)
	}

	return binPath, nil
}

func findBinaryAssetInDir(binary, destDir string) (string, error) {
	var paths []string
	if err := filepath.Walk(destDir,
		func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() {
				paths = append(paths, path)
			}
			return nil
		}); err != nil {
		return "", fmt.Errorf("unable to walk directory %q: %w", destDir, err)
	}

	log.WithFields("dir", destDir, "candidates", len(paths)).Trace("searching for binary asset in directory")

	ignore := strset.New("LICENSE", "README.md", checksumsFilename)
	var filteredPaths []string
	for _, p := range paths {
		if ignore.Has(filepath.Base(p)) {
			continue
		}
		filteredPaths = append(filteredPaths, p)
	}

	var binPath string
	switch len(filteredPaths) {
	case 0:
		return "", fmt.Errorf("no files found in %q", destDir)
	case 1:
		if binary != "" && binary != filepath.Base(filteredPaths[0]) {
			return "", fmt.Errorf("binary file %q not found in %q (found %q)", binary, destDir, filteredPaths[0])
		}

		binPath = filteredPaths[0]
	default:
		bp, err := filterMultipleArchiveBinaries(binary, destDir, filteredPaths, binPath)
		if err != nil {
			return "", err
		}
		binPath = bp
	}

	log.WithFields("file", binPath).Trace("found binary asset")

	return binPath, nil
}

func filterMultipleArchiveBinaries(binary string, destDir string, filteredPaths []string, binPath string) (string, error) {
	// do mime type detection to find only binaries
	var candidates []string
	for _, p := range filteredPaths {
		tyName, err := mimeTypeOfFile(p)
		if err != nil {
			log.WithFields("file", p).Tracef("unable to detect mime type: %s", err)
			continue
		}

		if binaryMimeTypes.Has(tyName) {
			candidates = append(candidates, p)
		}
	}

	switch len(candidates) {
	case 0:
		return "", fmt.Errorf("no binary files found in %q", destDir)
	case 1:
		if binary != "" && binary != filepath.Base(candidates[0]) {
			return "", fmt.Errorf("binary file %q not found in %q (found %q)", binary, destDir, candidates[0])
		}

		binPath = candidates[0]
	default:
		if binary != "" {
			for _, p := range candidates {
				if binary == filepath.Base(p) {
					binPath = p
				}
			}
		}
		if binPath == "" {
			return "", fmt.Errorf("multiple files found in %q", destDir)
		}
	}
	return binPath, nil
}

func mimeTypeOfFile(p string) (string, error) {
	mimeType, err := mimetype.DetectFile(p)
	if err != nil {
		return "", fmt.Errorf("unable to detect mime type: %s", err)
	}

	return strings.Split(mimeType.String(), ";")[0], nil
}

func selectChecksumAsset(lgr logger.Logger, assets []ghAsset) *ghAsset {
	// search for the asset by name with the OS and arch in the name
	// e.g. chronicle_0.7.0_checksums.txt

	lgr.Trace("looking for checksum artifact")

	for _, asset := range assets {
		switch strings.Split(asset.ContentType, ";")[0] {
		case "text/plain", "":
			// pass
		default:
			lgr.WithFields("asset", asset.Name).Tracef("skipping asset (content type %q can't be a checksum)", asset.ContentType)

			continue
		}

		lowerName := strings.ToLower(asset.Name)

		if !strings.HasSuffix(lowerName, checksumsFilename) {
			lgr.WithFields("asset", asset.Name).Trace("skipping asset (name does not indicate checksums)")
			continue
		}
		return &asset
	}
	return nil
}

// this list is derived from https://github.com/golang/go/blob/master/src/go/build/syslist.go
var architectureAliases = map[string][]string{
	"386":         {"i386", "x32", "x86"},
	"amd64":       {"x86_64", "86_64", "x86-64", "86-64"},
	"amd64p32":    {},
	"arm":         {},
	"arm64":       {"aarch64"},
	"arm64be":     {},
	"armbe":       {},
	"loong64":     {},
	"mips":        {},
	"mips64":      {},
	"mips64le":    {},
	"mips64p32":   {},
	"mips64p32le": {},
	"mipsle":      {},
	"ppc":         {},
	"ppc64":       {},
	"ppc64le":     {},
	"riscv":       {},
	"riscv64":     {},
	"s390":        {},
	"s390x":       {},
	"sparc":       {},
	"sparc64":     {},
	"wasm":        {},
}

// this list is derived from https://github.com/golang/go/blob/master/src/go/build/syslist.go
var osAliases = map[string][]string{
	"aix":       {},
	"android":   {},
	"darwin":    {"macos"},
	"dragonfly": {},
	"freebsd":   {},
	"hurd":      {},
	"illumos":   {},
	"ios":       {},
	"js":        {},
	"linux":     {},
	"nacl":      {},
	"netbsd":    {},
	"openbsd":   {},
	"plan9":     {},
	"solaris":   {},
	"wasip1":    {},
	"windows":   {},
	"zos":       {},
}

var (
	archKeys = strset.New(flattenAliases(architectureAliases)...)
	osKeys   = strset.New(flattenAliases(osAliases)...)
)

func flattenAliases(aliases map[string][]string) []string {
	var as []string
	for k, vs := range aliases {
		as = append(as, k)
		as = append(as, vs...)
	}
	return as
}

func selectBinaryAsset(lgr logger.Logger, assets []ghAsset, goOS, goArch string, assetPatterns []*regexp.Regexp) *ghAsset {
	// search for the asset by name with the OS and arch in the name
	// e.g. chronicle_0.7.0_linux_amd64.tar.gz

	goos := strings.ToLower(goOS)
	gooss := allOSs(goos)
	goarchs := allArchs(strings.ToLower(goArch))

	isHostDarwin := strset.New(allOSs("darwin")...).Has(goos)
	universalDarwinArchSuffix := asSuffix([]string{"universal", "all"})

	lgr.Trace("looking for binary artifact")

	// first pass: filter by content type, OS, and architecture
	var osArchCandidates []ghAsset
	for _, asset := range assets {
		switch {
		case isBinaryAsset(asset) || isArchiveAsset(asset):
			// pass
		default:
			lgr.WithFields("asset", asset.Name).Tracef("skipping asset (content type %q)", asset.ContentType)
			continue
		}

		cleanName := normalizedAssetName(asset.Name)

		if !containsOneOf(cleanName, asSuffix(gooss)) {
			lgr.WithFields("asset", asset.Name).Tracef("skipping asset (missing os %q)", gooss)
			continue
		}

		isUniversalDarwin := isHostDarwin && containsOneOf(cleanName, universalDarwinArchSuffix)
		if !isUniversalDarwin && !containsOneOf(cleanName, goarchs) {
			lgr.WithFields("asset", asset.Name).Tracef("skipping asset (missing arch %q)", goarchs)
			continue
		}

		osArchCandidates = append(osArchCandidates, asset)
	}

	if len(osArchCandidates) == 0 {
		return nil
	}

	// second pass: apply regex patterns if provided
	if len(assetPatterns) == 0 {
		// no asset patterns specified, return first matching asset
		selectedAsset := &osArchCandidates[0]
		lgr.WithFields("asset", selectedAsset.Name).Trace("found asset (no pattern filtering)")
		return selectedAsset
	}

	// try each pattern in order until we find a match
	for _, pattern := range assetPatterns {
		for _, candidate := range osArchCandidates {
			if pattern.MatchString(candidate.Name) {
				lgr.WithFields("asset", candidate.Name, "pattern", pattern.String()).Trace("found asset (pattern matched)")
				return &candidate
			}
		}
	}

	// no pattern matched
	lgr.Trace("no asset matched any of the specified patterns")
	return nil
}

func normalizedAssetName(name string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(name), ".", "_"), "-", "_")
}

func hasBinaryExtension(name string) bool {
	ext := filepath.Ext(name)
	switch ext {
	case ".exe", "":
		return true
	}

	cleanExt := normalizedAssetName(ext)
	fields := strings.Split(cleanExt, "_")
	// get the last field
	cleanExt = fields[len(fields)-1]

	if archKeys.Has(cleanExt) || osKeys.Has(cleanExt) {
		// this is a loose confirmation that the suffix is not a file extension
		return true
	}

	return false
}

func allArchs(key string) []string {
	candidates := []string{key}
	if aliases, ok := architectureAliases[key]; ok {
		candidates = append(candidates, aliases...)
	}
	return candidates
}

func allOSs(key string) []string {
	candidates := []string{key}
	if aliases, ok := osAliases[key]; ok {
		candidates = append(candidates, aliases...)
	}
	return candidates
}

func asSuffix(ss []string) []string {
	var suffixes []string
	for _, s := range ss {
		suffixes = append(suffixes, "_"+s)
	}
	return suffixes
}

func containsOneOf(subject string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(subject, needle) {
			return true
		}
	}
	return false
}

func fetchRelease(lgr logger.Logger, user, repo, tag string) (r *ghRelease, err error) {
	summary := fmt.Sprintf("%s/%s@%s", user, repo, tag)

	lgr.Trace("fetching release info")

	defer func() {
		if r == nil {
			lgr.Tracef("no release found")
			return
		}

		lgr.Tracef("release found with %d release assets", len(r.Assets))
	}()

	r, err = fetchReleaseByScrape(lgr, user, repo, tag)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch release %s via scrape: %w", summary, err)
	}

	if r != nil {
		return r, nil
	}

	lgr.Trace("unable to fetch release via scrape, trying checksums...")

	// why try this second instead of first? there are multiple reasons:
	// - there are multiple places to look for checksums
	// - there is no guarantee they even exist!
	r, err = fetchReleaseByChecksums(lgr, user, repo, tag)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch release %s via checksums: %w", summary, err)
	}

	if r != nil {
		return r, nil
	}

	lgr.Trace("unable to fetch release via checksums, trying GitHub v4 API...")

	// note: I would remove this approach, however, it is the most kosher way to get this information so I'm leaving it in for now.
	// It is quite unfortunate that either auth is required (v4) or there is extreme rate limiting (v3).
	r, err = fetchReleaseGithubV4API(user, repo, tag)
	if err != nil {
		return nil, fmt.Errorf("unable to fetch release %s via GitHub v4 API: %w", summary, err)
	}

	return r, nil
}

func fetchReleaseByChecksums(lgr logger.Logger, user, repo, tag string) (*ghRelease, error) {
	// look for a {checksums.txt, repo_tag_checksums.txt, repo_tag-without-v_checksums.txt} file in the release assets
	// if found, download it and parse it to find the asset we want
	// e.g.
	// - https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_checksums.txt (underscores)
	// - https://github.com/golangci/golangci-lint/releases/download/v1.54.2/golangci-lint-1.54.2-checksums.txt  (dashes)
	// - https://github.com/charmbracelet/glow/releases/download/v1.5.1/checksums.txt (no repo/version)

	for _, url := range checksumURLVariants(user, repo, tag) {
		lgr.WithFields("url", url).Trace("trying checksums url")
		reader, err := internal.DownloadURL(lgr, url)
		if err != nil {
			return nil, err
		}

		release := handleChecksumsReader(lgr, user, repo, tag, url, reader)
		if release == nil {
			continue
		}

		return release, nil
	}

	return nil, nil
}

func checksumURLVariants(user, repo, tag string) []string {
	dlURL := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s", user, repo, tag)

	tagWithoutPrefixV := strings.TrimPrefix(tag, "v")

	variants := strset.New(
		"checksums.txt",
		fmt.Sprintf("%s_%s_checksums.txt", repo, tagWithoutPrefixV),
		fmt.Sprintf("%s_%s_checksums.txt", repo, tag),
		fmt.Sprintf("%s-%s-checksums.txt", repo, tagWithoutPrefixV),
		fmt.Sprintf("%s-%s-checksums.txt", repo, tag),
	)

	variantsList := variants.List()
	sort.Strings(variantsList)

	var urls []string
	for _, variant := range variantsList {
		urls = append(urls, fmt.Sprintf("%s/%s", dlURL, variant))
	}

	return urls
}

func handleChecksumsReader(lgr logger.Logger, user, repo, tag, url string, reader io.ReadCloser) *ghRelease {
	if reader == nil {
		return nil
	}
	defer reader.Close()

	// parse output like this to create asset entries:
	//
	// 10ca05f5cfbac1b2c24a4a28b1f2a7446409769a74cc8a079a5c63bc2fbfb6e1  syft_0.93.0_linux_amd64.rpm
	// 169da07ce4cbe5f59ae3cc6a65b7b7b539ed07b987905e526d5fc4491ea0024e  syft_0.93.0_darwin_arm64.tar.gz
	// 193ff3ed631b5d5acbef575885a3417883c371f713bfead677a167f6ebe7603c  syft_0.93.0_linux_s390x.tar.gz
	// 1c1e3da7cec98e54720832a43fa1bed4e893e63a6be267d5ec55d62418535d2f  syft_0.93.0_linux_ppc64le.deb
	// 2ebf4167cbd499eb39119023d5f2e69b75af2223aea73115c5fc03e8a6e9e0c0  syft_0.93.0_linux_amd64.deb
	// 334bd4f1b41ef21f675bdb7113d32076377da6cced741c3365f76bdb7120ddac  syft_0.93.0_linux_arm64.rpm
	// 5fb0eb70c0f618e9a8b93d68b59da4b5758164b1aacc062e2150341baf7acc73  syft_0.93.0_linux_amd64.tar.gz
	// 64b31c2a078ac05889aa1f365afa8aa63f847b1750036cab19bba11a054e5fe3  syft_0.93.0_linux_ppc64le.tar.gz
	// 78da6446129fa3ae65114ddf8a56b7d581e21796fd7db8c0724d9ae8f8e3eeb4  syft_0.93.0_windows_amd64.zip
	// a40c32ecb52da7d9d7adf42f9321f73f179373d461685b168b0904d27cabed39  syft_0.93.0_linux_arm64.deb
	// b3b438990b043a0fe6f1b993ac3b88a2e0d7c2d98650156ec568b4754214662d  syft_0.93.0_linux_s390x.deb
	// b413c6b10815f2512a2f44b9a554521c376759e91ac411b157b6b44937e652a8  syft_0.93.0_linux_s390x.rpm
	// f2f8889305350ee3a53a012246acfa10b59b7aee67e9b6a2e811f05b67f74588  syft_0.93.0_linux_arm64.tar.gz
	// fbf8d99ff614221bdb78dc608dd4430b0fd04a56939a779818c7b296dfd470f1  syft_0.93.0_darwin_amd64.tar.gz
	// ff289b81c0f2bec792f2125ef0f3d7b78e70684b9fd4dcb3037f32c0c53b9328  syft_0.93.0_linux_ppc64le.rpm

	release := &ghRelease{
		Tag: tag,
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if len(line) == 0 {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) != 2 {
			lgr.WithFields("url", url).Trace("invalid checksums line: %q", line)
			return nil
		}
		name := fields[1]
		checksum := fields[0]
		asset := ghAsset{
			Name:        name,
			ContentType: "",
			URL:         fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", user, repo, tag, name),
		}
		asset.addChecksum(checksum)

		release.Assets = append(release.Assets, asset)
	}

	if len(release.Assets) == 0 {
		return nil
	}

	return release
}

func fetchReleaseByScrape(lgr logger.Logger, user, repo, tag string) (*ghRelease, error) {
	// fetch assets list via the expanded assets view endpoint used by the GitHub UI
	// note: this is quite brittle, super grain of salt here...
	// e.g. https://github.com/anchore/syft/releases/expanded_assets/v0.93.0
	// which will have href values like "/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_amd64.deb"

	url := fmt.Sprintf("https://github.com/%s/%s/releases/expanded_assets/%s", user, repo, tag)

	reader, err := internal.DownloadURL(lgr, url)
	if err != nil {
		return nil, err
	}

	if reader == nil {
		return nil, nil
	}

	defer reader.Close()

	return &ghRelease{
		Tag:    tag,
		Assets: processExpandedAssets(lgr, reader, url),
	}, nil
}

func processExpandedAssets(lgr logger.Logger, reader io.Reader, from string) []ghAsset {
	tokenizer := html.NewTokenizer(reader)

	var assets []ghAsset

	for {
		tokenType := tokenizer.Next()

		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if err == io.EOF {
				break
			}

			lgr.WithFields("error", tokenizer.Err()).Trace("error tokenizing html from %q", from)
		}

		switch tokenType {
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" && strings.Contains(attr.Val, "/releases/download/") {
						assets = append(assets, ghAsset{
							Name:        filepath.Base(attr.Val),
							ContentType: "",
							URL:         fmt.Sprintf("https://github.com%s", attr.Val),
						})
					}
				}
			}
		}
	}
	return assets
}

func fetchReleaseGithubV4API(user, repo, tag string) (*ghRelease, error) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN environment variable not set but is required to use the GitHub v4 API")
	}

	client := githubv4.NewClient(newRetryableGitHubClient(token))

	// TODO: act on hitting a rate limit
	type rateLimit struct {
		Cost      githubv4.Int
		Limit     githubv4.Int
		Remaining githubv4.Int
		ResetAt   githubv4.DateTime
	}

	var query struct {
		Repository struct {
			DatabaseID githubv4.Int
			URL        githubv4.URI
			Release    struct {
				TagName       githubv4.String
				IsLatest      githubv4.Boolean
				IsDraft       githubv4.Boolean
				PublishedAt   githubv4.DateTime
				ReleaseAssets struct {
					PageInfo struct {
						EndCursor   githubv4.String
						HasNextPage bool
					}
					Nodes []struct {
						Name        githubv4.String
						ContentType githubv4.String
						DownloadURL githubv4.URI
					}
				} `graphql:"releaseAssets(first:100, after:$assetsCursor)"`
			} `graphql:"release(tagName:$tagName)"`
		} `graphql:"repository(owner:$repositoryOwner, name:$repositoryName)"`

		RateLimit rateLimit
	}
	variables := map[string]interface{}{
		"repositoryOwner": githubv4.String(user),
		"repositoryName":  githubv4.String(repo),
		"tagName":         githubv4.String(tag),    // Null after argument to get first page.
		"assetsCursor":    (*githubv4.String)(nil), // Null after argument to get first page.
	}

	err := client.Query(context.Background(), &query, variables)
	if err != nil {
		return nil, err
	}

	var assets []ghAsset

	// TODO: go to the next page :) (was taking a while for cosign so need to investigate)
	// for {
	for _, a := range query.Repository.Release.ReleaseAssets.Nodes {
		// support charset spec, e.g. "text/plain; charset=utf-8""
		contentType := strings.Split(string(a.ContentType), ";")[0]

		assets = append(assets, ghAsset{
			Name:        string(a.Name),
			ContentType: contentType,
			URL:         a.DownloadURL.String(),
		})
	}

	// 	if !query.Repository.Release.ReleaseAssets.PageInfo.HasNextPage {
	// 		break
	// 	}
	// 	variables["assetsCursor"] = githubv4.NewString(query.Repository.Release.ReleaseAssets.PageInfo.EndCursor)
	// }

	return &ghRelease{
		Tag:      string(query.Repository.Release.TagName),
		IsLatest: boolRef(bool(query.Repository.Release.IsLatest)),
		IsDraft:  boolRef(bool(query.Repository.Release.IsDraft)),
		Date:     &query.Repository.Release.PublishedAt.Time,
		Assets:   assets,
	}, nil
}
