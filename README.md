# 🧠 Conversation Memory Engram

Keeps a bounded per-session conversation history and emits `conversation.context.v1`
packets for downstream prompts without requiring each Engram to implement its own
memory layer.

## 🌟 Highlights

- Maintains per-key message history with TTL-based eviction and optional deduplication.
- Supports both batch Jobs and streaming Deployments under the same Engram spec.
- Produces normalized context packets downstream Engrams can consume immediately.
- Offers configurable merge windows, rate-limited sweeps, and flexible role mapping.

## 🚀 Quick Start

```bash
go vet ./...
go test ./...
docker build -t ghcr.io/bubustack/conversation-memory-engram:dev .
```

Deploy the resulting container as part of a bobrapet Story. Use the sample
`Engram.yaml` to register the template and pick defaults for your project.

## ⚙️ Configuration (`Engram.spec.with`)

| Field | Default | Description |
| --- | --- | --- |
| `maxMessages` | `16` | Maximum history length stored per key. |
| `sessionTTL` | `45m` | Idle TTL before conversation keys are swept. |
| `sweepInterval` | `1m` | How often the engram reaps expired keys. |
| `minUserChars` | `4` | Minimum user text length that contributes to history. |
| `ignorePattern` | `(?i)^(mhm+|hmm+|uh+|um+|ok(?:ay)?|mm+|huh)[.!? ]*$` | Regex of filler utterances to ignore for `role=user`. |
| `dedupeWindow` | `1500ms` | Suppresses duplicate role-text pairs within this window. |
| `mergeWindowMs` | `3000` | Merges consecutive same-role packets arriving within this window. |
| `defaultRole` | `user` | Role used when metadata omits a role. |

## 📥 Inputs

| Field | Description |
| --- | --- |
| `key` | Explicit conversation key. If omitted, the engram derives one from session or participant metadata. |
| `sessionId` | Optional session identifier used when deriving the conversation key. |
| `role` | `user`, `assistant`, `developer`, or `system` (defaults to `defaultRole`). |
| `text` / `assistantText` | Message content to add to history. |
| `speakerId` | Optional identifier used for deduplication and merge logic. |
| `reset` | Boolean flag to drop existing history for the current key before processing the message. |
| `maxMessages` | Per-request override for the retained history length. |
| `minUserChars` | Per-request override for the minimum accepted user transcript length. |
| `includeHistory` | When `false`, suppresses the `history` array in the emitted context packet. |

Additional passthrough fields such as `metadata` are preserved for downstream context assembly because the input schema allows extra properties.

## 📤 Outputs

Emits a `conversation.context.v1` payload containing:

- `accepted`: boolean indicating the message was accepted into history.
- `role`, `text`, `speakerId`, and timestamps from the last stored message.
- `history`: trimmed list of prior messages (bounded by `maxMessages`).
- `metadata`: merged metadata from the stored history and the latest packet.
- `diagnostics`: when `BUBU_DEBUG=true`, exposes TTL and sweep metadata.

## 🧪 Local Development

- `go vet ./...` – Runs Go vet checks.
- `go test ./...` – Unit tests covering merging, dedup, and hydration.
- `docker build -t ghcr.io/bubustack/conversation-memory-engram:dev .` – Builds the container image.

## 🤝 Community & Support

- [Contributing](./CONTRIBUTING.md)
- [Support](./SUPPORT.md)
- [Security Policy](./SECURITY.md)
- [Code of Conduct](./CODE_OF_CONDUCT.md)
- [Discord](https://discord.gg/dysrB7D8H6)

## 📄 License

Copyright 2025 BubuStack.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
