package githubrelease

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/anchore/go-logger"
	"github.com/anchore/go-logger/adapter/discard"
)

func TestInstaller_InstallTo(t *testing.T) {
	testTag := "1.0.0"
	binaryPath := filepath.Join("testdata", "archive-contents", "flat", "syft")
	binaryAssetName := fmt.Sprintf("syft_%s_%s_%s", testTag, runtime.GOOS, runtime.GOARCH)
	expectedChecksum := "688cf0875c5cc1c7d3a26249e48e8fa9f8cb61b79bdde593bfda6e4c367a692e"

	setup := func(checksum string) func(lgr logger.Logger, user, repo, tag string) (*ghRelease, error) {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.Contains(r.URL.Path, "syft_"):
				by, err := os.ReadFile(binaryPath)
				require.NoError(t, err)
				_, err = w.Write(by)
				require.NoError(t, err)
			case strings.Contains(r.URL.Path, "checksums.txt"):
				if checksum == "" {
					t.Fatal("checksum was not provided, but was requested")
				}
				contents := fmt.Sprintf("%s  %s", checksum, binaryAssetName)
				_, err := w.Write([]byte(contents))
				require.NoError(t, err)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}

			return
		}))
		t.Cleanup(s.Close)

		return func(_ logger.Logger, user, repo, tag string) (*ghRelease, error) {
			assets := []ghAsset{
				{
					Name:        binaryAssetName,
					ContentType: "application/octet-stream",
					URL:         s.URL + "/" + binaryAssetName,
				},
			}
			if checksum != "" {
				assets = append(assets, ghAsset{
					Name:        "checksums.txt",
					ContentType: "text/plain; charset=utf-8",
					URL:         s.URL + "/checksums.txt",
				})
			}

			theTime := time.Now()

			return &ghRelease{
				Tag:      testTag,
				Date:     &theTime,
				IsLatest: boolRef(true),
				IsDraft:  boolRef(false),
				Assets:   assets,
			}, nil
		}
	}

	tests := []struct {
		name           string
		releaseFetcher func(lgr logger.Logger, user, repo, tag string) (*ghRelease, error)
		wantErr        require.ErrorAssertionFunc
	}{
		{
			name:           "single binary without checksum",
			releaseFetcher: setup(""),
		},
		{
			name:           "single binary with valid checksum",
			releaseFetcher: setup(expectedChecksum),
		},
		{
			name:           "single binary with valid checksum",
			releaseFetcher: setup("bad-checksum!"),
			wantErr:        require.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destDir := t.TempDir()
			version := testTag

			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			i := NewInstaller(
				InstallerParameters{
					Repo: "anchore/syft",
				},
			)
			i.releaseFetcher = tt.releaseFetcher

			expectedDownloadPath := filepath.Join(destDir, binaryAssetName)

			got, err := i.InstallTo(version, destDir)
			tt.wantErr(t, err)

			if err != nil {
				require.Equal(t, "", got)
				return
			}
			require.Equal(t, expectedDownloadPath, got)

			// ensure the contents match
			expected, err := os.ReadFile(binaryPath)
			require.NoError(t, err)
			actual, err := os.ReadFile(expectedDownloadPath)
			require.NoError(t, err)

			assert.Equal(t, expected, actual)
		})
	}
}

func Test_getChecksumForAsset(t *testing.T) {
	type args struct {
	}
	tests := []struct {
		name          string
		assetName     string
		checksumsPath string
		want          string
		wantErr       require.ErrorAssertionFunc
	}{
		{
			name:          "happy path",
			assetName:     "gosimports_0.3.8_windows_arm64.tar.gz",
			checksumsPath: "testdata/checksums.txt",
			want:          "95e760adf2d0545c0aa982f2bf8cd3f0358d13307e5ca153de4eb9fabc9d72b7",
		},
		{
			name:          "asset not found",
			assetName:     "notfound",
			checksumsPath: "testdata/checksums.txt",
			want:          "",
		},
		{
			name:          "testdata/checksums-not-found.txt",
			assetName:     "notfound",
			checksumsPath: "testdata/does-not-exist.txt",
			want:          "",
			wantErr:       require.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := getChecksumForAsset(tt.assetName, tt.checksumsPath)
			tt.wantErr(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_extractArchive(t *testing.T) {
	tarPath, expectedBinName := createTestArchive(t)

	tests := []struct {
		name        string
		archivePath string
		destDir     string
		wantName    string
		wantErr     require.ErrorAssertionFunc
	}{
		{
			name:        "happy path",
			archivePath: tarPath,
			destDir:     t.TempDir(),
			wantName:    expectedBinName,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}

			expectedBinPath := filepath.Join(tt.destDir, tt.wantName)

			got, err := extractArchive(tt.archivePath, tt.destDir)
			tt.wantErr(t, err)

			assert.Equal(t, expectedBinPath, got)
		})
	}
}

func createTestArchive(t *testing.T) (string, string) {
	archivePath := filepath.Join(t.TempDir(), "test_fixture.tar.gz")
	archiveFile, err := os.Create(archivePath)
	require.NoError(t, err)

	defer archiveFile.Close()

	gzipWriter := gzip.NewWriter(archiveFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// create plain text files
	plainTextFiles := []struct {
		Name    string
		Content string
	}{
		{"file1.txt", "This is the content of file 1."},
		{"file2.txt", "This is the content of file 2."},
		{"file3.txt", "This is the content of file 3."},
	}

	for _, file := range plainTextFiles {
		header := &tar.Header{
			Name: file.Name,
			Size: int64(len(file.Content)),
			Mode: 0644,
		}
		require.NoError(t, tarWriter.WriteHeader(header))
		_, err := tarWriter.Write([]byte(file.Content))
		require.NoError(t, err)
	}

	// create a binary file
	binaryFilename := "binary_file.bin"
	binaryContent := []byte{0x03, 0x4B, 0x04, 0x0A, 0x50, 0x4B, 0x03, 0x50, 0x04, 0x0A, 0x00, 0x00, 0x00, 0x00, 0x00}
	binaryHeader := &tar.Header{
		Name: binaryFilename,
		Size: int64(len(binaryContent)),
		Mode: 0755,
	}
	require.NoError(t, tarWriter.WriteHeader(binaryHeader))
	_, err = tarWriter.Write(binaryContent)
	require.NoError(t, err)

	return archivePath, binaryFilename
}

func Test_findBinaryAssetInDir(t *testing.T) {
	tests := []struct {
		name    string
		destDir string
		want    string
		wantErr require.ErrorAssertionFunc
	}{
		{
			name:    "flat assets",
			destDir: "testdata/archive-contents/flat",
			want:    "testdata/archive-contents/flat/syft",
		},
		{
			name:    "nested assets",
			destDir: "testdata/archive-contents/nested",
			want:    "testdata/archive-contents/nested/syft/syft",
		},
		{
			name:    "multiple binaries",
			destDir: "testdata/archive-contents/multiple-bins",
			wantErr: require.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr == nil {
				tt.wantErr = require.NoError
			}
			got, err := findBinaryAssetInDir(tt.destDir)
			tt.wantErr(t, err)

			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_selectChecksumAsset(t *testing.T) {
	tests := []struct {
		name   string
		assets []ghAsset
		want   *ghAsset
	}{
		{
			name: "no assets",
		},
		{
			name: "no checksums file",
			assets: []ghAsset{
				{
					Name:        "some-file.txt",
					ContentType: "text/plain",
					URL:         "http://localhost:8080/some-file.txt",
				},
			},
		},
		{
			name: "select standard checksums file",
			assets: []ghAsset{
				{
					Name:        "checksums.txt",
					ContentType: "text/plain",
					URL:         "http://localhost:8080/checksums.txt",
				},
			},
			want: &ghAsset{
				Name:        "checksums.txt",
				ContentType: "text/plain",
				URL:         "http://localhost:8080/checksums.txt",
			},
		},
		{
			name: "select checksums file with asset name",
			assets: []ghAsset{
				{
					Name:        "chronicle_0.7.0_checksums.txt",
					ContentType: "text/plain; charset=utf-8", // note: there is a charset too
					URL:         "http://localhost:8080/chronicle_0.7.0_checksums.txt",
				},
			},
			want: &ghAsset{
				Name:        "chronicle_0.7.0_checksums.txt",
				ContentType: "text/plain; charset=utf-8",
				URL:         "http://localhost:8080/chronicle_0.7.0_checksums.txt",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, selectChecksumAsset(discard.New(), tt.assets))
		})
	}
}

func Test_selectBinaryAsset(t *testing.T) {
	type args struct {
		assets []ghAsset
		goOS   string
		goArch string
	}
	tests := []struct {
		name string
		args args
		want *ghAsset
	}{
		{
			name: "no assets",
			args: args{
				assets: nil,
				goOS:   "linux",
				goArch: "amd64",
			},
			want: nil,
		},
		{
			name: "no binary assets for target host",
			args: args{
				goOS:   "linux",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_linux_amd64.rpm",
						ContentType: "application/x-rpm",
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64.rpm",
					},
					{
						Name:        "syft_0.89.0_linux_amd64.deb",
						ContentType: "application/x-debian-package",
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64.deb",
					},
					{
						Name:        "syft_0.89.0_linux_amd64.spdx.json",
						ContentType: "text/plain",
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64.spdx.json",
					},
					{
						Name:        "syft_0.89.0_windows_amd64.msi",
						ContentType: "application/x-msi",
						URL:         "http://localhost:8080/syft_0.89.0_windows_amd64.msi",
					},
				},
			},
			want: nil,
		},
		{
			name: "binary assets executable (by content type)",
			args: args{
				goOS:   "linux",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_linux_amd64",
						ContentType: "application/x-executable",
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_linux_amd64",
				ContentType: "application/x-executable",
				URL:         "http://localhost:8080/syft_0.89.0_linux_amd64",
			},
		},
		{
			name: "binary assets executable (by lack of extension)",
			args: args{
				goOS:   "linux",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_linux_amd64",
						ContentType: "", // important!
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_linux_amd64",
				ContentType: "", // important!
				URL:         "http://localhost:8080/syft_0.89.0_linux_amd64",
			},
		},
		{
			name: "binary assets executable (by lack of extension) - regression",
			args: args{
				goOS:   "linux",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name: "yajsv.darwin.amd64",
					},
					{
						Name: "yajsv.darwin.arm64",
					},
					{
						Name: "yajsv.linux.386",
					},
					{
						Name: "yajsv.linux.amd64",
					},
					{
						Name: "yajsv.windows.386.exe",
					},
					{
						Name: "yajsv.windows.amd64.exe",
					},
				},
			},
			want: &ghAsset{
				Name: "yajsv.linux.amd64",
			},
		},
		{
			name: "binary assets executable (by extension) - regression",
			args: args{
				goOS:   "windows",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name: "yajsv.darwin.amd64",
					},
					{
						Name: "yajsv.darwin.arm64",
					},
					{
						Name: "yajsv.linux.386",
					},
					{
						Name: "yajsv.linux.amd64",
					},
					{
						Name: "yajsv.windows.386.exe",
					},
					{
						Name: "yajsv.windows.amd64.exe",
					},
				},
			},
			want: &ghAsset{
				Name: "yajsv.windows.amd64.exe",
			},
		},
		{
			name: "binary assets executable (by extension)",
			args: args{
				goOS:   "windows",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_windows_amd64.exe",
						ContentType: "", // important!
						URL:         "http://localhost:8080/syft_0.89.0_windows_amd64.exe",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_windows_amd64.exe",
				ContentType: "", // important!
				URL:         "http://localhost:8080/syft_0.89.0_windows_amd64.exe",
			},
		},
		{
			name: "binary assets tar.gz",
			args: args{
				goOS:   "linux",
				goArch: "amd64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_linux_amd64.tar.gz",
						ContentType: "application/gzip",
						URL:         "http://localhost:8080/syft_0.89.0_linux_amd64.tar.gz",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_linux_amd64.tar.gz",
				ContentType: "application/gzip",
				URL:         "http://localhost:8080/syft_0.89.0_linux_amd64.tar.gz",
			},
		},
		{
			name: "alt arch and os name",
			args: args{
				goOS:   "darwin",
				goArch: "arm64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_macos_aarch64.tar.gz",
						ContentType: "application/gzip",
						URL:         "http://localhost:8080/syft_0.89.0_macos_aarch64.tar.gz",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_macos_aarch64.tar.gz",
				ContentType: "application/gzip",
				URL:         "http://localhost:8080/syft_0.89.0_macos_aarch64.tar.gz",
			},
		},
		{
			name: "consider by extension name instead of content type",
			args: args{
				goOS:   "darwin",
				goArch: "arm64",
				assets: []ghAsset{
					{
						Name:        "syft_0.89.0_macos_aarch64.tar.gz",
						ContentType: "", // important!
						URL:         "http://localhost:8080/syft_0.89.0_macos_aarch64.tar.gz",
					},
				},
			},
			want: &ghAsset{
				Name:        "syft_0.89.0_macos_aarch64.tar.gz",
				ContentType: "", // important!
				URL:         "http://localhost:8080/syft_0.89.0_macos_aarch64.tar.gz",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equalf(t, tt.want, selectBinaryAsset(discard.New(), tt.args.assets, tt.args.goOS, tt.args.goArch), "selectBinaryAsset(%v, %v, %v)", tt.args.assets, tt.args.goOS, tt.args.goArch)
		})
	}
}

func Test_checksumURLVariants(t *testing.T) {

	tests := []struct {
		name string
		user string
		repo string
		tag  string
		want []string
	}{
		{
			name: "happy path",
			user: "anchore",
			repo: "syft",
			tag:  "v1.0.0",
			want: []string{
				"https://github.com/anchore/syft/releases/download/v1.0.0/checksums.txt",
				"https://github.com/anchore/syft/releases/download/v1.0.0/syft-1.0.0-checksums.txt",
				"https://github.com/anchore/syft/releases/download/v1.0.0/syft-v1.0.0-checksums.txt",
				"https://github.com/anchore/syft/releases/download/v1.0.0/syft_1.0.0_checksums.txt",
				"https://github.com/anchore/syft/releases/download/v1.0.0/syft_v1.0.0_checksums.txt",
			},
		},
		{
			name: "no v prefix on tag",
			user: "anchore",
			repo: "syft",
			tag:  "1.0.0",
			want: []string{
				"https://github.com/anchore/syft/releases/download/1.0.0/checksums.txt",
				"https://github.com/anchore/syft/releases/download/1.0.0/syft-1.0.0-checksums.txt",
				"https://github.com/anchore/syft/releases/download/1.0.0/syft_1.0.0_checksums.txt",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, checksumURLVariants(tt.user, tt.repo, tt.tag))
		})
	}
}

func Test_handleChecksumsReader(t *testing.T) {
	tests := []struct {
		name     string
		user     string
		repo     string
		tag      string
		url      string
		contents string
		want     *ghRelease
	}{
		{
			name: "empty",
			user: "anchore",
			repo: "syft",
			tag:  "v0.93.0",
			url:  "https://github.com/anchore/syft/releases/download/1.0.0/checksums.txt",
			contents: `

`,

			want: nil,
		},
		{
			name: "happy path",
			user: "anchore",
			repo: "syft",
			tag:  "v0.93.0",
			url:  "https://github.com/anchore/syft/releases/download/1.0.0/checksums.txt",
			contents: `

10ca05f5cfbac1b2c24a4a28b1f2a7446409769a74cc8a079a5c63bc2fbfb6e1  syft_0.93.0_linux_amd64.rpm
169da07ce4cbe5f59ae3cc6a65b7b7b539ed07b987905e526d5fc4491ea0024e  syft_0.93.0_darwin_arm64.tar.gz

`,

			want: &ghRelease{
				Tag: "v0.93.0",
				Assets: []ghAsset{
					{
						Name:     "syft_0.93.0_linux_amd64.rpm",
						URL:      "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_amd64.rpm",
						Checksum: "sha256:10ca05f5cfbac1b2c24a4a28b1f2a7446409769a74cc8a079a5c63bc2fbfb6e1",
					},
					{
						Name:     "syft_0.93.0_darwin_arm64.tar.gz",
						URL:      "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_darwin_arm64.tar.gz",
						Checksum: "sha256:169da07ce4cbe5f59ae3cc6a65b7b7b539ed07b987905e526d5fc4491ea0024e",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := cmp.Diff(tt.want, handleChecksumsReader(discard.New(), tt.user, tt.repo, tt.tag, tt.url, io.NopCloser(strings.NewReader(tt.contents))))
			if d != "" {
				t.Log(d)
			}
			assert.Equal(t, tt.want, handleChecksumsReader(discard.New(), tt.user, tt.repo, tt.tag, tt.url, io.NopCloser(strings.NewReader(tt.contents))))
		})
	}
}

func Test_processExpandedAssets(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    []ghAsset
	}{
		{
			name:    "syft example",
			fixture: "testdata/expandedAssets.html",
			want: []ghAsset{
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_checksums.txt",
					Name: "syft_0.93.0_checksums.txt",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_darwin_amd64.tar.gz",
					Name: "syft_0.93.0_darwin_amd64.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_darwin_arm64.tar.gz",
					Name: "syft_0.93.0_darwin_arm64.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_amd64.deb",
					Name: "syft_0.93.0_linux_amd64.deb",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_amd64.rpm",
					Name: "syft_0.93.0_linux_amd64.rpm",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_amd64.tar.gz",
					Name: "syft_0.93.0_linux_amd64.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_arm64.deb",
					Name: "syft_0.93.0_linux_arm64.deb",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_arm64.rpm",
					Name: "syft_0.93.0_linux_arm64.rpm",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_arm64.tar.gz",
					Name: "syft_0.93.0_linux_arm64.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_ppc64le.deb",
					Name: "syft_0.93.0_linux_ppc64le.deb",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_ppc64le.rpm",
					Name: "syft_0.93.0_linux_ppc64le.rpm",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_ppc64le.tar.gz",
					Name: "syft_0.93.0_linux_ppc64le.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_s390x.deb",
					Name: "syft_0.93.0_linux_s390x.deb",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_s390x.rpm",
					Name: "syft_0.93.0_linux_s390x.rpm",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_linux_s390x.tar.gz",
					Name: "syft_0.93.0_linux_s390x.tar.gz",
				},
				{
					URL:  "https://github.com/anchore/syft/releases/download/v0.93.0/syft_0.93.0_windows_amd64.zip",
					Name: "syft_0.93.0_windows_amd64.zip",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fh, err := os.Open(tt.fixture)
			require.NoError(t, err)
			assert.Equal(t, tt.want, processExpandedAssets(discard.New(), fh, "my-url"))
		})
	}
}

func Test_hasArchiveExtension(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{
			name: "syft_0.93.0_linux_amd64.tar.gz",
			want: true,
		},
		{
			name: "syft_0.93.0_linux_amd64.tar",
			want: true,
		},
		{
			name: "syft_0.93.0_linux_amd64.tgz",
			want: true,
		},
		{
			name: "thing.gz.does",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasArchiveExtension(tt.name))
		})
	}
}

func Test_hasKnownNonBinaryExtension(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// positive cases
		{
			name: "syft_0.93.0_linux_amd64.tar.gz",
			want: true,
		},
		{
			name: "syft_0.93.0_linux_amd64.tar",
			want: true,
		},
		{
			name: "syft_0.93.0_linux_amd64.tgz",
			want: true,
		},
		{
			name: "checksums.txt",
			want: true,
		},
		{
			name: "checksums.pem",
			want: true,
		},
		{
			name: "checksums.sig",
			want: true,
		},
		// negative cases...
		{
			name: "thing.gz.does",
			want: false,
		},
		{
			name: "yajsv.darwin.amd64",
			want: false,
		},
		{
			name: "yajsv.darwin.arm64",
			want: false,
		},
		{
			name: "yajsv.linux.386",
			want: false,
		},
		{
			name: "yajsv.linux.amd64",
			want: false,
		},
		{
			name: "yajsv.windows.386.exe",
			want: false,
		},
		{
			name: "yajsv.windows.amd64.exe",
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, hasKnownNonBinaryExtension(tt.name))
		})
	}
}

func Test_isBinaryAsset(t *testing.T) {
	tests := []struct {
		name  string
		asset ghAsset
		want  bool
	}{
		{
			name: "binary by content type",
			asset: ghAsset{
				Name:        "thing.tar.gz",             // important! mismatch with content type
				ContentType: "application/x-executable", // this is the reason for the match
			},
			want: true,
		},
		{
			name: "binary by extension",
			asset: ghAsset{
				Name:        "thing.exe",
				ContentType: "", // important!
			},
			want: true,
		},
		{
			name: "binary by non extension",
			asset: ghAsset{
				Name:        "thing",
				ContentType: "", // important!
			},
			want: true,
		},
		{
			name: "not binary by extension",
			asset: ghAsset{
				Name:        "thing.tar",
				ContentType: "", // important!
			},
			want: false,
		},
		{
			name: "not binary by content type",
			asset: ghAsset{
				Name:        "thing", // important! cannot have extension
				ContentType: "application/x-lzx",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isBinaryAsset(tt.asset))
		})
	}
}

func Test_isArchiveAsset(t *testing.T) {
	tests := []struct {
		name  string
		asset ghAsset
		want  bool
	}{
		{
			name: "archive by content type",
			asset: ghAsset{
				Name:        "thing.tar.gz",     // important! mismatch with content type
				ContentType: "application/gzip", // this is the reason for the match
			},
			want: true,
		},
		{
			name: "archive by extension",
			asset: ghAsset{
				Name:        "thing.tar",
				ContentType: "", // important!
			},
			want: true,
		},
		{
			name: "not archive by non extension",
			asset: ghAsset{
				Name:        "thing",
				ContentType: "", // important!
			},
			want: false,
		},
		{
			name: "not archive by extension",
			asset: ghAsset{
				Name:        "thing.md",
				ContentType: "", // important!
			},
			want: false,
		},
		{
			name: "not archive by content type",
			asset: ghAsset{
				Name:        "thing", // important! cannot have extension
				ContentType: "application/x-sharedlib",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isArchiveAsset(tt.asset))
		})
	}
}
