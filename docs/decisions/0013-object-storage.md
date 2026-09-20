# 0013 — Object storage: a neutral Bucket, one provider per backend

Date: 2026-09-20
Status: accepted

## Context

Services store uploads and generated files in object storage and must not
proxy the bytes: browsers upload and download through presigned URLs. The
SDKs are heavy (AWS, Google Cloud), and a service uses one of them.

## Decision

- `pkg/storage` in the root module holds what is backend-neutral and
  stdlib-only: the `Bucket` interface (`Put`, `Get`, `Stat`, `Delete`,
  `PresignPut`, `PresignGet`), its option types, `ErrNotFound`, and signed
  upload tokens. It is the one `pkg/*` package services import directly,
  because its types are the API.
- Each backend is its own provider module so a service links one SDK:
  `providers/s3` covers AWS and every S3-compatible server (MinIO, R2);
  a Google Cloud Storage provider implements the same interface when a
  service needs it. A service that picks its backend from config chooses
  which `Enable` to pass, like any role switch.
- `providers/s3` checks the bucket at boot with the configured credentials
  and reports it in `/readyz`. Path-style addressing is automatic when an
  endpoint is set. Static credentials are optional; without them the AWS
  default chain applies.
- Direct uploads are constrained by the signature: content type, MD5 and
  length given to `PresignPut` are signed headers, so the store refuses
  anything else. The handshake is completed by `storage.Signer`: the
  service signs `{key, subject, expiry, meta}` when it presigns, and
  verifies it when the client submits the upload. The client never names a
  key, and a token cannot be redeemed by another subject.
- The wire format of the handshake belongs to the service. gox provides the
  primitives; a service whose frontend speaks an existing protocol (for
  example a framework's direct-upload JSON) maps it onto them.

## Consequences

- Presigned URLs default to 15 minutes and cannot exceed the 7 days S3
  signs.
- Integration tests need an S3-compatible server (`S3_TEST_ENDPOINT`, MinIO
  in a container) and are skipped without one.
