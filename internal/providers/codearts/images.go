package codearts

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/caigee-cmd/cli2api/internal/accounts"
	"github.com/caigee-cmd/cli2api/internal/providers"
	"github.com/caigee-cmd/cli2api/internal/translate"
)

// The inference endpoint only accepts inline data: URLs for image parts.
// Remote http(s) references fail with InferHub.ModelArts.81001.400
// "unsupported image url", and hosted-agent clients (Qoder/WorkBuddy custom
// endpoints) legitimately send signed object-storage links for their
// attachments. The adapter therefore downloads each remote image once and
// rewrites the part to base64 before the request is mapped.
const (
	imageDownloadTimeout = 45 * time.Second
	imageMaxBytes        = 20 << 20
	imageCacheTTL        = 10 * time.Minute
	imageCacheMaxEntries = 16
	imageCacheMaxBytes   = 64 << 20
)

type imageCacheEntry struct {
	dataURL string
	bytes   int
	expires time.Time
}

// imageCache remembers inlined images briefly so replayed conversation turns
// do not re-download the same attachment on every request.
type imageCache struct {
	mu      sync.Mutex
	order   []string
	entries map[string]imageCacheEntry
}

func newImageCache() *imageCache {
	return &imageCache{entries: map[string]imageCacheEntry{}}
}

func (c *imageCache) get(key string, now time.Time) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || now.After(entry.expires) {
		return "", false
	}
	return entry.dataURL, true
}

func (c *imageCache) put(key, dataURL string, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]imageCacheEntry{}
	}
	if _, exists := c.entries[key]; !exists {
		c.order = append(c.order, key)
	}
	c.entries[key] = imageCacheEntry{dataURL: dataURL, bytes: len(dataURL), expires: now.Add(imageCacheTTL)}
	total := 0
	for _, entry := range c.entries {
		total += entry.bytes
	}
	for len(c.order) > 0 && (len(c.order) > imageCacheMaxEntries || total > imageCacheMaxBytes) {
		oldest := c.order[0]
		c.order = c.order[1:]
		if entry, ok := c.entries[oldest]; ok {
			total -= entry.bytes
			delete(c.entries, oldest)
		}
	}
}

// inlineMessageImages rewrites every remote image URL in the request into an
// inline data: URL. Parts already carrying data: URLs are left untouched, and
// messages without images cost one type assertion each.
func (c *Client) inlineMessageImages(ctx context.Context, accountID string, messages []translate.ChatMessage) error {
	if !messagesHaveRemoteImage(messages) {
		return nil
	}
	client, err := c.httpClient(ctx, accountID)
	if err != nil {
		return err
	}
	var firstErr error
	for index := range messages {
		items, ok := messages[index].Content.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(stringValue(block["type"])))
			if kind != "image_url" && kind != "input_image" && kind != "image" {
				continue
			}
			url := imageURLFromPart(block)
			if !isRemoteImageURL(url) {
				continue
			}
			dataURL, err := inlineImageURL(ctx, client, c.images, url)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			rewriteImagePartURL(block, dataURL)
		}
	}
	return firstErr
}

func messagesHaveRemoteImage(messages []translate.ChatMessage) bool {
	for _, message := range messages {
		items, ok := message.Content.([]any)
		if !ok {
			continue
		}
		for _, item := range items {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			kind := strings.ToLower(strings.TrimSpace(stringValue(block["type"])))
			if kind != "image_url" && kind != "input_image" && kind != "image" {
				continue
			}
			if isRemoteImageURL(imageURLFromPart(block)) {
				return true
			}
		}
	}
	return false
}

func isRemoteImageURL(rawURL string) bool {
	return strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://")
}

func rewriteImagePartURL(block map[string]any, dataURL string) {
	switch value := block["image_url"].(type) {
	case map[string]any:
		value["url"] = dataURL
	case string:
		block["image_url"] = dataURL
	default:
		block["image_url"] = map[string]any{"url": dataURL}
	}
}

// inlineImageURL returns a data: URL for the given image reference. data: URLs
// pass through untouched; http(s) URLs are downloaded through the provided
// client and base64-encoded with a sniffed or declared MIME type.
func inlineImageURL(ctx context.Context, client *http.Client, cache *imageCache, rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || strings.HasPrefix(rawURL, "data:") || !isRemoteImageURL(rawURL) {
		return rawURL, nil
	}
	now := time.Now()
	if cache != nil {
		if dataURL, ok := cache.get(rawURL, now); ok {
			return dataURL, nil
		}
	}
	if client == nil {
		return "", &providers.Error{
			Kind:    accounts.KindInvalidRequest,
			Status:  http.StatusBadRequest,
			Message: "codearts image part is a remote URL but no HTTP client is available to inline it",
		}
	}
	downloadCtx, cancel := context.WithTimeout(ctx, imageDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", wrapImageError(rawURL, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", wrapImageError(rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", wrapImageError(rawURL, fmt.Errorf("download returned %s", resp.Status))
	}
	payload, err := io.ReadAll(io.LimitReader(resp.Body, imageMaxBytes+1))
	if err != nil {
		return "", wrapImageError(rawURL, err)
	}
	if len(payload) > imageMaxBytes {
		return "", wrapImageError(rawURL, fmt.Errorf("image exceeds %d bytes", imageMaxBytes))
	}
	dataURL := "data:" + imageMimeType(resp, req.URL.Path, payload) + ";base64," + base64.StdEncoding.EncodeToString(payload)
	if cache != nil {
		cache.put(rawURL, dataURL, now)
	}
	return dataURL, nil
}

// imageMimeType prefers the served Content-Type, then content sniffing, then
// the URL extension, so re-encoded or mislabeled object storage still yields a
// usable type instead of defaulting everything to png.
func imageMimeType(resp *http.Response, urlPath string, payload []byte) string {
	if mimeType := strings.TrimSpace(resp.Header.Get("Content-Type")); strings.HasPrefix(mimeType, "image/") {
		if semicolon := strings.Index(mimeType, ";"); semicolon > 0 {
			mimeType = mimeType[:semicolon]
		}
		return strings.TrimSpace(mimeType)
	}
	if detected := http.DetectContentType(payload); strings.HasPrefix(detected, "image/") {
		return detected
	}
	if guessed := mime.TypeByExtension(strings.ToLower(path.Ext(urlPath))); guessed != "" {
		return guessed
	}
	return "image/png"
}

func wrapImageError(rawURL string, err error) error {
	host := rawURL
	if parsed, parseErr := url.Parse(rawURL); parseErr == nil && parsed.Host != "" {
		host = parsed.Host
	}
	return &providers.Error{
		Kind:    accounts.KindInvalidRequest,
		Status:  http.StatusBadRequest,
		Message: fmt.Sprintf("codearts upstream only accepts inline images and the remote image could not be fetched (%s): %v", host, err),
	}
}
