// Package asana is a minimal client for the Asana REST API.
package asana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// DefaultBaseURL is the Asana REST API root.
const DefaultBaseURL = "https://app.asana.com/api/1.0"

const maxRetryWait = 10 * time.Second

var gidPattern = regexp.MustCompile(`^[0-9]+$`)

// ValidGID reports whether s looks like an Asana gid.
func ValidGID(s string) bool { return gidPattern.MatchString(s) }

// Client calls the Asana API with a personal access token.
type Client struct {
	BaseURL string
	token   string
	http    *http.Client
	sleep   func(time.Duration)
}

// New returns a client that authenticates with token.
func New(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
		sleep:   time.Sleep,
	}
}

// APIError is a non-2xx response from Asana.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string { return fmt.Sprintf("asana: HTTP %d: %s", e.Status, e.Message) }

func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(map[string]any{"data": body})
		if err != nil {
			return err
		}
		payload = b
	}
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	for attempt := 0; ; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, target, reader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt == 0 {
			if wait := retryAfter(resp.Header.Get("Retry-After")); wait <= maxRetryWait {
				c.sleep(wait)
				continue
			}
		}
		if resp.StatusCode >= 300 {
			return apiError(resp.StatusCode, data)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(data, out)
	}
}

func retryAfter(header string) time.Duration {
	if s, err := strconv.Atoi(header); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	return time.Second
}

func apiError(status int, body []byte) error {
	var e struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	msg := http.StatusText(status)
	if json.Unmarshal(body, &e) == nil && len(e.Errors) > 0 && e.Errors[0].Message != "" {
		msg = e.Errors[0].Message
	}
	return &APIError{Status: status, Message: msg}
}

type nextPage struct {
	Offset string `json:"offset"`
}

func getAll[T any](ctx context.Context, c *Client, path string, q url.Values) ([]T, error) {
	q = maps.Clone(q)
	if q == nil {
		q = url.Values{}
	}
	q.Set("limit", "100")
	var all []T
	for {
		var page struct {
			Data     []T       `json:"data"`
			NextPage *nextPage `json:"next_page"`
		}
		if err := c.do(ctx, http.MethodGet, path, q, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		if page.NextPage == nil || page.NextPage.Offset == "" {
			return all, nil
		}
		q.Set("offset", page.NextPage.Offset)
	}
}

func getOne[T any](ctx context.Context, c *Client, path string, q url.Values) (T, error) {
	var r struct {
		Data T `json:"data"`
	}
	err := c.do(ctx, http.MethodGet, path, q, nil, &r)
	return r.Data, err
}
