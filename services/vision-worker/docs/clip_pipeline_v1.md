# Vision Clip V1

Vision Clip V1 is an opt-in fixed-duration pipeline. It aggregates one final
`synora.vision.clip-summary/v1` observation per track. It does not call the
MLP, State Encoder v5, Device Store, automation, notification or action
surfaces.

Configuration defaults are deliberately conservative:

| Setting | Environment/config default | Meaning |
| --- | --- | --- |
| max clip duration | `10s` | hard upper bound for one clip |
| episode continuity window | `5s` | reuses `episode_id` for a near consecutive clip |
| max crops per track | `5` | only the best local ROI references are retained |
| critical alert threshold | `0.90` | strong critical sensitive-object result required |

The existing detector is currently a person detector. The V1 adapter therefore
emits `human` only when that detector is used; it does not infer `animal` or
`vehicle`. Those subject types remain supported by the interface and dry-run
fakes.

Human enrichment is uncertain when face detection, landmark alignment or the
configured recognition backend is unavailable or insufficient. It is never
converted to `unknown`. Vehicle enrichment returns `not_available` until a
real plate detector/OCR backend is configured. Sensitive-object enrichment
returns `not_available` until a real model is configured.

The laboratory SFace file
`/home/rock/synora-rknn-v1-lab/sface_2021dec_rk3588_fp.rknn` is not connected by
this change. The deployed default remains the existing explicitly configured
ArcFace path. SFace can be added behind the `FaceEnricher` interface after an
independent RKNN validation proves aligned `1x3x112x112` input and `float32
[1,128]` output; no model is downloaded or substituted silently.

References on the bus are local opaque references only. Raw images, raw
embeddings and plain plate text are never put in the event payload.


