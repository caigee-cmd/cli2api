package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	desktopReleasesURL = "https://docs.devin.ai/desktop/releases.md"
	desktopBinaryPath  = "Devin/resources/app/extensions/windsurf/bin/language_server_linux_x64"
	maxReleaseBytes    = 4 << 20
	maxArchiveBytes    = 512 << 20
	maxExpandedBytes   = 2 << 30
	maxBinaryBytes     = 256 << 20
)

var desktopReleaseRE = regexp.MustCompile(`(?s)<Update\s+[^>]*\blabel="([^"]+)"[^>]*>.*?<Release\s+[^>]*\blinuxX64="([^"]+)"[^>]*/>`)
var desktopVersionRE = regexp.MustCompile(`^v?([0-9]+\.[0-9]+\.[0-9]+)$`)

type desktopSource struct {
	Version, URL, BinaryPath, ArchiveSHA256, BinarySHA256 string
}

func downloadDesktop(ctx context.Context, version, workDir string) (desktopSource, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	releases, err := getBody(ctx, client, desktopReleasesURL, maxReleaseBytes)
	if err != nil {
		return desktopSource{}, fmt.Errorf("fetch Devin Desktop releases: %w", err)
	}
	match, err := selectDesktopRelease(string(releases), version)
	if err != nil {
		return desktopSource{}, err
	}
	archiveURL := match.url
	u, err := url.Parse(archiveURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "windsurf-stable.codeiumdata.com" {
		return desktopSource{}, fmt.Errorf("invalid Linux archive URL for %s", match.version)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL, nil)
	if err != nil {
		return desktopSource{}, fmt.Errorf("create archive request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return desktopSource{}, fmt.Errorf("download Devin Desktop %s: %w", match.version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return desktopSource{}, fmt.Errorf("download Devin Desktop %s: %s", match.version, resp.Status)
	}
	if resp.ContentLength > maxArchiveBytes {
		return desktopSource{}, fmt.Errorf("Devin Desktop archive exceeds %d-byte limit", maxArchiveBytes)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return desktopSource{}, fmt.Errorf("create work directory: %w", err)
	}
	tmp, err := os.CreateTemp(workDir, ".language_server-*")
	if err != nil {
		return desktopSource{}, fmt.Errorf("create binary destination: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	archiveHash := sha256.New()
	counted := &countingReader{Reader: io.LimitReader(resp.Body, maxArchiveBytes+1)}
	gz, err := gzip.NewReader(io.TeeReader(counted, archiveHash))
	if err != nil {
		tmp.Close()
		return desktopSource{}, fmt.Errorf("open Devin Desktop archive: %w", err)
	}
	expanded := &countingReader{Reader: io.LimitReader(gz, maxExpandedBytes+1)}
	binaryHash, err := extractDesktopBinary(tar.NewReader(expanded), tmp)
	if closeErr := tmp.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return desktopSource{}, err
	}
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return desktopSource{}, fmt.Errorf("validate Devin Desktop archive: %w", err)
	}
	if expanded.N > maxExpandedBytes {
		return desktopSource{}, fmt.Errorf("Devin Desktop archive expands beyond %d-byte limit", maxExpandedBytes)
	}
	if _, err := io.Copy(io.Discard, io.TeeReader(counted, archiveHash)); err != nil {
		return desktopSource{}, fmt.Errorf("finish Devin Desktop download: %w", err)
	}
	if counted.N > maxArchiveBytes {
		return desktopSource{}, fmt.Errorf("Devin Desktop archive exceeds %d-byte limit", maxArchiveBytes)
	}
	binaryPath := filepath.Join(workDir, "language_server")
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		return desktopSource{}, fmt.Errorf("install extracted language server: %w", err)
	}
	return desktopSource{match.version, archiveURL, binaryPath, hex.EncodeToString(archiveHash.Sum(nil)), hex.EncodeToString(binaryHash)}, nil
}

type desktopRelease struct{ version, url string }

func selectDesktopRelease(releases, version string) (desktopRelease, error) {
	requested := strings.TrimPrefix(version, "v")
	if version != "" && !desktopVersionRE.MatchString(requested) {
		return desktopRelease{}, fmt.Errorf("invalid Devin Desktop version %q", version)
	}
	matches := desktopReleaseRE.FindAllStringSubmatch(releases, -1)
	for _, match := range matches {
		label := desktopVersionRE.FindStringSubmatch(match[1])
		if len(label) != 2 {
			continue
		}
		if version == "" || label[1] == requested {
			return desktopRelease{label[1], match[2]}, nil
		}
	}
	if version == "" {
		return desktopRelease{}, errors.New("no Linux x64 stable Devin Desktop release found")
	}
	return desktopRelease{}, fmt.Errorf("Devin Desktop version %q was not found", version)
}

func getBody(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d-byte limit", limit)
	}
	return body, nil
}

func extractDesktopBinary(tr *tar.Reader, dst io.Writer) ([]byte, error) {
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in Devin Desktop archive", desktopBinaryPath)
		}
		if err != nil {
			return nil, fmt.Errorf("read Devin Desktop archive: %w", err)
		}
		if hdr.Name != desktopBinaryPath {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("%s is not a regular file", desktopBinaryPath)
		}
		if hdr.Size < 0 || hdr.Size > maxBinaryBytes {
			return nil, fmt.Errorf("%s exceeds %d-byte limit", desktopBinaryPath, maxBinaryBytes)
		}
		hash := sha256.New()
		n, err := io.Copy(io.MultiWriter(dst, hash), io.LimitReader(tr, hdr.Size))
		if err == nil && n != hdr.Size {
			err = io.ErrUnexpectedEOF
		}
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", desktopBinaryPath, err)
		}
		return hash.Sum(nil), nil
	}
}

type countingReader struct {
	io.Reader
	N int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.N += int64(n)
	return n, err
}
