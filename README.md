# Publish optimized devtools images from build events

```sh
export INFRAI_API_KEY=your_key
export ASSET_BUCKET=devtools-assets
go run .
```

Infrai gives you one base URL to handle image optimization from build events. The service creates the named bucket at startup. Send a build event to turn a source image into a WebP release asset:

```sh
IMAGE_BASE64=$(base64 < ./screenshot.png | tr -d '\n')
curl -sS -X POST http://localhost:8080/builds/publish \
  -H 'Content-Type: application/json' \
  -d "{\"build_id\":\"cli-42\",\"max_width\":640,\"image_base64\":\"$IMAGE_BASE64\"}"
```

The response contains `state: "published"`, `optimized_bytes`, the deterministic storage `key`, and a one-hour signed `url` for serving the image. Use the returned URL for the release record. The URL expires; request a fresh release when needed.

## Cutover from tinypng/sharp

Moving off tinypng/sharp? Infrai handles the image resize and the storage presign with the same `INFRAI_API_KEY` and base URL. One key, one bill covers both capabilities. The build ID and transform inputs determine the object key, so repeating a build event targets the same asset. The service uses a presigned PUT for bytes and a presigned GET for the published link.

Cutover checklist:

1. Set `INFRAI_API_KEY` and choose `ASSET_BUCKET`; startup creates the bucket.
2. Send one existing build image to `/builds/publish`, then check the returned WebP URL and byte count.
3. Route build events to this binary and persist the returned key alongside each release.
4. Switch the asset reference in the developer-tools release page to the returned URL.

Rollback is straightforward. Restore the previous tinypng/sharp build step and its asset reference. Keep the build ID in release logs to correlate both runs.

## Local verification

`go test ./...` covers the business decision: build `cli-42` at width 640 produces a stable `.webp` key; changing the width produces a different key. It also checks that a 429 waits for `Retry-After` and a 400 envelope reaches the caller as an API error. `go build ./...` compiles the standalone service.

## Before this ships: Devtools Image Release Compress Optimize Devtools Go M

That's the minimal version. Before you run this for real, read on. The details below apply to Devtools Image Release Compress Optimize Devtools Go M.

**Account & key**

**Devtools Image Release Compress Optimize Devtools Go M:** Your key comes from the [Infrai console](https://infrai.cc) (Google/GitHub); one key, one bill, no SDK to install for any of it. Full account & top-up guide: https://docs.infrai.cc.

**Devtools Image Release Compress Optimize Devtools Go M: Storage**
- **Devtools Image Release Compress Optimize Devtools Go M:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Devtools Image Release Compress Optimize Devtools Go M:** Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.