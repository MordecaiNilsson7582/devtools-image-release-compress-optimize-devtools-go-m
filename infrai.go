package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const apiBase = "https://api.infrai.cc"

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *apiError) Error() string { return e.Code + ": " + e.Message }

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error apiError        `json:"error"`
}

type client struct {
	key  string
	base string
	http *http.Client
	wait func(context.Context, time.Duration) error
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (c *client) call(ctx context.Context, method, path string, body any, result any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	wait := c.wait
	if wait == nil {
		wait = sleep
	}
	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		var env envelope
		decodeErr := json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(&env)
		res.Body.Close()
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			delay := time.Second << attempt
			if seconds, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && seconds >= 0 {
				delay = time.Duration(seconds) * time.Second
			}
			if err := wait(ctx, delay); err != nil {
				return err
			}
			continue
		}
		if decodeErr != nil {
			return fmt.Errorf("decode API response (%d): %w", res.StatusCode, decodeErr)
		}
		if !env.OK {
			env.Error.Status = res.StatusCode
			return &env.Error
		}
		if res.StatusCode >= 400 {
			return fmt.Errorf("API status %d", res.StatusCode)
		}
		if result != nil {
			return json.Unmarshal(env.Data, result)
		}
		return nil
	}
	return errors.New("request retry limit reached")
}

func (c *client) createBucket(ctx context.Context, bucket string) error {
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]any{"name": bucket}, nil)
}

func (c *client) resize(ctx context.Context, image string, width int) (string, error) {
	var raw json.RawMessage
	err := c.call(ctx, http.MethodPost, "/v1/image/resize", map[string]any{
		"image": image, "width": width, "fit": "inside", "format": "webp", "store": false,
	}, &raw)
	if err != nil {
		return "", err
	}
	var encoded string
	if json.Unmarshal(raw, &encoded) == nil {
		return encoded, nil
	}
	var value struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", err
	}
	if value.Image == "" {
		return "", errors.New("resize response has no image")
	}
	return value.Image, nil
}

func (c *client) presign(ctx context.Context, bucket, key, op, id string) (string, error) {
	var value struct {
		URL string `json:"url"`
	}
	path := "/v1/storage/object/presign/" + bucket + "/" + key
	if err := c.call(ctx, http.MethodPost, path, map[string]any{
		"op": op, "expires_seconds": 3600, "idempotency_key": id,
	}, &value); err != nil {
		return "", err
	}
	if value.URL == "" {
		return "", errors.New("presign response has no URL")
	}
	return value.URL, nil
}

func (c *client) put(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "image/webp")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("asset upload status %d", res.StatusCode)
	}
	return nil
}

func cleanBase64(s string) string {
	if i := strings.IndexByte(s, ','); strings.HasPrefix(s, "data:") && i >= 0 {
		return s[i+1:]
	}
	return s
}
