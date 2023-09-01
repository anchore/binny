package githubrelease

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstaller_InstallTo(t *testing.T) {
	testTag := "1.0.0"
	binaryPath := filepath.Join("testdata", "archive-contents", "flat", "syft")
	binaryAssetName := fmt.Sprintf("syft_%s_%s_%s", testTag, runtime.GOOS, runtime.GOARCH)
	expectedChecksum := "688cf0875c5cc1c7d3a26249e48e8fa9f8cb61b79bdde593bfda6e4c367a692e"

	setup := func(checksum string) func(user, repo, tag string) (*ghRelease, error) {
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

		return func(user, repo, tag string) (*ghRelease, error) {
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
			return &ghRelease{
				Tag:      testTag,
				Date:     time.Now(),
				IsLatest: true,
				IsDraft:  false,
				Assets:   assets,
			}, nil
		}
	}

	tests := []struct {
		name           string
		releaseFetcher func(user, repo, tag string) (*ghRelease, error)
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
			assert.Equal(t, tt.want, selectChecksumAsset(tt.assets))
		})
	}
}
