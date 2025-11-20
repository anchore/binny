package internal

import (
	"crypto/md5"  //nolint:gosec // MD5 is used for legacy compatibility
	"crypto/sha1" //nolint:gosec // SHA1 is used for legacy compatibility
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/hash"

	"github.com/anchore/go-logger"
)

func DownloadFile(lgr logger.Logger, url string, filepath string, checksum string) (err error) {
	reader, err := DownloadURL(lgr, url)
	if err != nil {
		return err
	}
	defer reader.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	// hash the file and compare with checksum while copying to disk
	h := getHasher(checksum)
	tee := io.TeeReader(reader, h)

	if _, err := io.Copy(out, tee); err != nil {
		return err
	}

	if checksum != "" {
		expectedChecksum := cleanChecksum(checksum)
		actualChecksum := fmt.Sprintf("%x", h.Sum(nil))

		if expectedChecksum != actualChecksum {
			lgr.WithFields("url", url, "expected", expectedChecksum, "actual", actualChecksum).Warn("checksum mismatch")
			return fmt.Errorf("checksum mismatch for %q", filepath)
		}

		lgr.WithFields("checksum", expectedChecksum, "asset", filepath, "url", url).Trace("checksum verified")
	}

	return nil
}

func DownloadURL(lgr logger.Logger, url string) (io.ReadCloser, error) {
	resp, err := http.Get(url) //nolint: gosec  // we must be able to get arbitrary URLs
	if err != nil {
		return nil, fmt.Errorf("unable to download %q: %w", url, err)
	}

	lgr.WithFields("http-status", resp.StatusCode).Tracef("http get %q", url)

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status code %d for %q", resp.StatusCode, url)
	}
	return resp.Body, nil
}

func cleanChecksum(checksum string) string {
	parts := strings.SplitN(checksum, ":", 2)
	if len(parts) < 2 {
		return checksum
	}

	return parts[1]
}

func getHasher(checksum string) hash.Hash {
	// Default to SHA-256 if no prefix or unsupported prefix
	defaultHash := sha256.New()

	parts := strings.SplitN(checksum, ":", 2)
	if len(parts) < 2 {
		return defaultHash
	}

	algorithm := strings.ToLower(parts[0])

	switch algorithm {
	case "sha256":
		return sha256.New()
	case "sha1":
		return sha1.New() //nolint:gosec // SHA1 is used for legacy compatibility
	case "sha512":
		return sha512.New()
	case "md5":
		return md5.New() //nolint:gosec // MD5 is used for legacy compatibility
	default:
		return defaultHash
	}
}
