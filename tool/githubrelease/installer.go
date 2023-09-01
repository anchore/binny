package githubrelease

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gabriel-vasile/mimetype"
	"github.com/mholt/archiver/v3"
	"github.com/scylladb/go-set/strset"
	"github.com/shurcooL/githubv4"
	"golang.org/x/oauth2"

	"github.com/anchore/binny"
	"github.com/anchore/binny/internal"
	"github.com/anchore/binny/internal/log"
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
	Repo string `json:"repo" yaml:"repo" mapstructure:"repo"`
}

type Installer struct {
	config         InstallerParameters
	releaseFetcher func(user, repo, tag string) (*ghRelease, error)
}

func NewInstaller(cfg InstallerParameters) Installer {
	return Installer{
		config:         cfg,
		releaseFetcher: fetchRelease,
	}
}

func (i Installer) InstallTo(version, destDir string) (string, error) {
	log.WithFields("repo", i.config.Repo, "version", version).Debug("installing from github release assets")

	fields := strings.Split(i.config.Repo, "/")
	if len(fields) != 2 {
		return "", fmt.Errorf("invalid github repo format: %q", i.config.Repo)
	}
	user, repo := fields[0], fields[1]

	release, err := i.releaseFetcher(user, repo, version)
	if err != nil {
		return "", fmt.Errorf("unable to fetch github release %s@%s: %w", i.config.Repo, version, err)
	}

	asset := selectBinaryAsset(release.Assets, runtime.GOOS, runtime.GOARCH)
	if asset == nil {
		return "", fmt.Errorf("unable to find matching asset for %s@%s", i.config.Repo, version)
	}

	checksumAsset := selectChecksumAsset(release.Assets)

	binPath, err := downloadAndExtractAsset(*asset, checksumAsset, destDir)
	if err != nil {
		return "", fmt.Errorf("unable to download and extract asset %s@%s: %w", i.config.Repo, version, err)
	}

	return binPath, nil
}

func downloadAndExtractAsset(asset ghAsset, checksumAsset *ghAsset, destDir string) (string, error) {
	assetPath := path.Join(destDir, asset.Name)

	log.WithFields("destination", assetPath).Trace("downloading asset")

	var checksum string
	if checksumAsset != nil {
		log.WithFields("asset", checksumAsset.Name).Trace("downloading checksum manifest")

		checksumsPath := path.Join(destDir, checksumsFilename)

		if err := internal.DownloadFile(checksumAsset.URL, checksumsPath, ""); err != nil {
			return "", fmt.Errorf("unable to download checksum asset %q: %w", checksumAsset.Name, err)
		}

		var err error
		checksum, err = getChecksumForAsset(asset.Name, checksumsPath)
		if err != nil {
			return "", fmt.Errorf("unable to get checksum for asset %q: %w", asset.Name, err)
		}
	}

	if err := internal.DownloadFile(asset.URL, assetPath, checksum); err != nil {
		return "", fmt.Errorf("unable to download asset %q: %w", asset.Name, err)
	}

	// check if it exists
	v, err := os.Stat(assetPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("asset %q does not exist", assetPath)
	}

	log.WithFields("size", v.Size(), "asset", asset.Name).Trace("downloaded asset")

	switch {
	case archiveMimeTypes.Has(asset.ContentType):
		log.WithFields("asset", asset.Name).Trace("asset is an archive")
		return extractArchive(assetPath, destDir)
	case binaryMimeTypes.Has(asset.ContentType):
		log.WithFields("asset", asset.Name).Trace("asset is a binary")
		return assetPath, nil
	}

	return "", fmt.Errorf("unsupported asset content-type: %q", asset.ContentType)
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

func extractArchive(archivePath, destDir string) (string, error) {
	// extract tar.gz to destDir
	if err := archiver.Unarchive(archivePath, destDir); err != nil {
		return "", fmt.Errorf("unable to extract asset %q: %w", archivePath, err)
	}

	if err := os.Remove(archivePath); err != nil {
		return "", fmt.Errorf("unable to remove asset archive %q: %w", archivePath, err)
	}

	// look for the binary recursively in the destDir and return that
	binPath, err := findBinaryAssetInDir(destDir)
	if err != nil {
		return "", fmt.Errorf("unable to find binary in %q: %w", destDir, err)
	}

	return binPath, nil
}

func findBinaryAssetInDir(destDir string) (string, error) {
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
		if ignore.Has(path.Base(p)) {
			continue
		}
		filteredPaths = append(filteredPaths, p)
	}

	var binPath string
	switch len(filteredPaths) {
	case 0:
		return "", fmt.Errorf("no files found in %q", destDir)
	case 1:
		binPath = filteredPaths[0]
	default:
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
			binPath = candidates[0]
		default:
			return "", fmt.Errorf("multiple files found in %q", destDir)
		}
	}

	log.WithFields("file", binPath).Trace("found binary asset")

	return binPath, nil
}

func mimeTypeOfFile(p string) (string, error) {
	mimeType, err := mimetype.DetectFile(p)
	if err != nil {
		return "", fmt.Errorf("unable to detect mime type: %s", err)
	}

	return strings.Split(mimeType.String(), ";")[0], nil
}

func selectChecksumAsset(assets []ghAsset) *ghAsset {
	// search for the asset by name with the OS and arch in the name
	// e.g. chronicle_0.7.0_checksums.txt

	for _, asset := range assets {
		switch strings.Split(asset.ContentType, ";")[0] {
		case "text/plain":
			// pass
		default:
			log.WithFields("asset", asset.Name).Tracef("skipping asset (content type %q can't be a checksum)", asset.ContentType)

			continue
		}

		lowerName := strings.ToLower(asset.Name)

		if !strings.HasSuffix(lowerName, checksumsFilename) {
			log.WithFields("asset", asset.Name).Trace("skipping asset (name does not indicate checksums)")
			continue
		}
		return &asset
	}
	return nil
}

// this list is derived from https://github.com/golang/go/blob/master/src/go/build/syslist.go
var architectureAliases = map[string][]string{
	"386":         {"i386"},
	"amd64":       {"x86_64"},
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

func allArchs(key string) []string {
	architectures := []string{key}
	if aliases, ok := architectureAliases[key]; ok {
		architectures = append(architectures, aliases...)
	}
	return architectures
}

func selectBinaryAsset(assets []ghAsset, goOS, goArch string) *ghAsset {
	// search for the asset by name with the OS and arch in the name
	// e.g. chronicle_0.7.0_linux_amd64.tar.gz

	goos := strings.ToLower(goOS)
	goarchs := allArchs(strings.ToLower(goArch))

	for _, asset := range assets {
		switch {
		case archiveMimeTypes.Has(asset.ContentType):
			// pass
		case binaryMimeTypes.Has(asset.ContentType):
			// pass
		default:
			log.WithFields("asset", asset.Name).Tracef("skipping asset (content type %q)", asset.ContentType)

			continue
		}

		lowerName := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(asset.Name), ".", "_"), "-", "_")

		if !strings.Contains(lowerName, "_"+goos) {
			log.WithFields("asset", asset.Name).Tracef("skipping asset (missing os %q)", goos)
			continue
		}

		// look for universal binaries for darwin
		if goos == "darwin" && strings.Contains(lowerName, "_universal") || strings.Contains(lowerName, "_all") {
			log.WithFields("asset", asset.Name).Trace("found asset (universal binary)")
			return &asset
		} else if !containsOneOf(lowerName, goarchs) {
			log.WithFields("asset", asset.Name).Tracef("skipping asset (missing arch %q)", goarchs)
			continue
		}

		log.WithFields("asset", asset.Name).Trace("found asset")
		return &asset
	}
	return nil
}

func containsOneOf(subject string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(subject, needle) {
			return true
		}
	}
	return false
}

// nolint:funlen
func fetchRelease(user, repo, tag string) (*ghRelease, error) {
	src := oauth2.StaticTokenSource(
		// TODO: DI this
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	httpClient := oauth2.NewClient(context.Background(), src)
	client := githubv4.NewClient(httpClient)

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
		IsLatest: bool(query.Repository.Release.IsLatest),
		IsDraft:  bool(query.Repository.Release.IsDraft),
		Date:     query.Repository.Release.PublishedAt.Time,
		Assets:   assets,
	}, nil
}
