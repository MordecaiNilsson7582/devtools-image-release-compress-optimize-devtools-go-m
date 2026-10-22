package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
)

var slug = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type buildEvent struct {
	BuildID string `json:"build_id"`
	Asset   string `json:"image_base64"`
	Width   int    `json:"max_width"`
}

type release struct {
	BuildID string `json:"build_id"`
	Key     string `json:"key"`
	URL     string `json:"url"`
	Bytes   int    `json:"optimized_bytes"`
	State   string `json:"state"`
}

func objectKey(e buildEvent) string {
	digest := sha256.Sum256([]byte(e.Asset + ":" + strconv.Itoa(e.Width)))
	return "build-" + e.BuildID + "-" + hex.EncodeToString(digest[:8]) + ".webp"
}

type service struct {
	api    *client
	bucket string
}

func (s service) publish(ctx context.Context, e buildEvent) (release, error) {
	if !slug.MatchString(e.BuildID) || e.Width < 1 || e.Width > 8192 || e.Asset == "" {
		return release{}, errors.New("build_id, image_base64 and max_width (1..8192) are required")
	}
	key := objectKey(e)
	image, err := s.api.resize(ctx, e.Asset, e.Width)
	if err != nil {
		return release{}, err
	}
	data, err := base64.StdEncoding.DecodeString(cleanBase64(image))
	if err != nil {
		return release{}, fmt.Errorf("decode optimized image: %w", err)
	}
	putURL, err := s.api.presign(ctx, s.bucket, key, "put", "put-"+key)
	if err != nil {
		return release{}, err
	}
	if err := s.api.put(ctx, putURL, data); err != nil {
		return release{}, err
	}
	getURL, err := s.api.presign(ctx, s.bucket, key, "get", "get-"+key)
	if err != nil {
		return release{}, err
	}
	return release{e.BuildID, key, getURL, len(data), "published"}, nil
}

func (s service) handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var event buildEvent
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	result, err := s.publish(r.Context(), event)
	if err != nil {
		status := http.StatusBadRequest
		var apiErr *apiError
		if errors.As(err, &apiErr) {
			status = http.StatusBadGateway
			if apiErr.Status >= 400 && apiErr.Status < 500 {
				status = apiErr.Status
			}
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	bucket := os.Getenv("ASSET_BUCKET")
	if key == "" || !slug.MatchString(bucket) {
		log.Fatal("set INFRAI_API_KEY and ASSET_BUCKET (letters, digits, _ or -)")
	}
	api := &client{key: key, base: apiBase, http: &http.Client{}}
	if err := api.createBucket(context.Background(), bucket); err != nil {
		var apiErr *apiError
		if !errors.As(err, &apiErr) || apiErr.Status != http.StatusConflict {
			log.Fatal(err)
		}
	}
	http.HandleFunc("/builds/publish", (service{api, bucket}).handler)
	log.Print("listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
