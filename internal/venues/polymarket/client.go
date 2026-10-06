package polymarket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/venues"
)

// API names, used as the RawResponse endpoint prefix (D-055).
const (
	apiGamma = "gamma"
	apiClob  = "clob"
)

// errTooLarge marks a response body over the configured cap.
var errTooLarge = errors.New("response body exceeds the size cap")

// httpError is a non-2xx response that survived the retries.
type httpError struct {
	Endpoint string
	Status   int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("GET %s: HTTP %d", e.Endpoint, e.Status)
}

// client performs capped, retried GETs against one Polymarket API.
type client struct {
	api          string
	base         *url.URL
	http         *http.Client
	maxBodyBytes int64
	retries      int
	backoff      time.Duration
	logger       *slog.Logger
	now          func() time.Time
}

// get fetches path?query with up to c.retries retries on 429, 5xx and
// transport errors, using exponential backoff. It returns the raw response of
// the last attempt (2xx or not) whenever a response was received, so callers
// can store it; err is non-nil for anything but a 2xx.
func (c *client) get(ctx context.Context, path string, query url.Values) (venues.RawResponse, error) {
	endpoint := c.api + "/" + strings.TrimPrefix(path, "/")
	if len(query) > 0 {
		endpoint += "?" + query.Encode() // Encode sorts keys, so endpoints are stable fixture names
	}
	u := *c.base
	u.Path = strings.TrimSuffix(u.Path, "/") + "/" + strings.TrimPrefix(path, "/")
	u.RawQuery = query.Encode()

	var (
		raw venues.RawResponse
		err error
	)
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			wait := c.backoff << (attempt - 1)
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return venues.RawResponse{}, fmt.Errorf("GET %s: %w", endpoint, ctx.Err())
			case <-timer.C:
			}
		}
		var retry bool
		raw, retry, err = c.do(ctx, u.String(), endpoint)
		if err == nil || !retry || ctx.Err() != nil {
			return raw, err
		}
		c.logger.Warn("polymarket request failed",
			"api", c.api, "endpoint", endpoint, "attempt", attempt+1, "status", raw.HTTPStatus, "error", err.Error())
	}
	return raw, err
}

// do performs one GET. retry reports whether the failure is worth retrying.
func (c *client) do(ctx context.Context, rawURL, endpoint string) (raw venues.RawResponse, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return venues.RawResponse{}, false, fmt.Errorf("building GET %s: %w", endpoint, err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return venues.RawResponse{}, ctx.Err() == nil, fmt.Errorf("GET %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBodyBytes+1))
	if err != nil {
		return venues.RawResponse{}, ctx.Err() == nil, fmt.Errorf("reading GET %s: %w", endpoint, err)
	}
	if int64(len(body)) > c.maxBodyBytes {
		// Drop the body: it is neither stored nor parsed.
		return venues.RawResponse{}, false, fmt.Errorf("GET %s: %w (%d bytes)", endpoint, errTooLarge, c.maxBodyBytes)
	}
	raw = venues.RawResponse{
		Venue:      canonical.VenuePolymarket,
		Endpoint:   endpoint,
		HTTPStatus: resp.StatusCode,
		Body:       body,
		FetchedAt:  c.now().UTC(),
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return raw, false, nil
	}
	retry = resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500
	return raw, retry, &httpError{Endpoint: endpoint, Status: resp.StatusCode}
}
