# Google Chat Bridge

Attach a Google Chat 1:1 direct message (DM) to a live Sagittarius session as a
second renderer alongside the TUI, or run headlessly as a standalone chat bot
service under `systemd` or `screen`.

Inbound events arrive via Google Cloud Pub/Sub pull subscription — **no inbound
ports, public IPs, or webhooks are needed**.

---

## Architecture

Sagittarius uses an internal Hub (`internal/ui/hub`) that acts as a single UI
layer to the agent runner while multiplexing events across both the terminal TUI
and the Google Chat DM.

```
                  ┌──────────────┐
                  │ agent.Runner │
                  └──────┬───────┘
                         │
                  ┌──────▼───────┐
                  │   ui/hub     │
                  └──┬────────┬──┘
                     │        │
         RenderStream│        │RenderStream
     (cross-echo user)        │(cross-echo user)
                     │        │
             ┌───────▼──┐  ┌──▼──────────┐
             │ ui/term  │  │ ui/googlechat│
             │ (Bubble  │  └──────┬──────┘
             │   Tea)   │         │
             └──────────┘         │ Pub/Sub pull (inbound)
                                  │ REST API (outbound)
                                  │
                           ┌──────▼──────┐
                           │ Google Chat │
                           └─────────────┘
```

- **Input serialization:** A bounded queue (capacity 5) prevents overlapping turns.
- **Cross-echo with attribution:** User prompts sent in terminal echo to Google Chat as `(via Terminal) ...`, and prompts sent in Google Chat echo to the terminal as `(via Google Chat) ...`.
- **Zero double-rendering:** Agent turns are streamed directly to the originating interface while finished messages are mirrored to the other.

---

## Prerequisites & Google Cloud Setup

### 1. Create a Google Cloud Project & Enable APIs
1. Go to the [Google Cloud Console](https://console.cloud.google.com/).
2. Create or select a project (e.g. `my-sagittarius-project`).
3. Enable the **Google Chat API** (`chat.googleapis.com`) and **Cloud Pub/Sub API** (`pubsub.googleapis.com`).

### 2. Configure the Google Chat App
In the Google Cloud Console, navigate to **Google Chat API** → **Configuration**:
- **Application info:**
  - **App name:** `Sagittarius` (or your preferred bot name)
  - **Avatar URL:** (optional image URL)
  - **Description:** `AI Coding and System Assistant`
- **Interactive features:**
  - Check **Enable interactive features**.
  - Under **Functionality**, select **Receive 1:1 messages** (do NOT enable group spaces).
  - Under **Connection settings**, select **Cloud Pub/Sub**.
  - Specify a Pub/Sub topic name, e.g. `projects/my-sagittarius-project/topics/sagittarius-chat`.
- **Visibility:**
  - Under **Availability**, select **Specific people and groups in your domain**.
  - Enter only your own Google Workspace / Google account email.

### 3. Create Pub/Sub Topic and Pull Subscription
In **Pub/Sub** → **Subscriptions**:
1. Create a pull subscription (e.g. `sagittarius-chat-sub`) attached to your topic (`sagittarius-chat`).
2. Grant the Google Chat service account permission to publish to your topic:
   - Topic permissions: Add `chat-api-push@system.gserviceaccount.com` with the role **Pub/Sub Publisher**.

### 4. Create Service Account Credentials
1. In **IAM & Admin** → **Service Accounts**, create a service account (e.g. `sagittarius-bot`).
2. Grant the service account the role **Pub/Sub Subscriber** (`roles/pubsub.subscriber`).
3. Create a JSON key for this service account and save it securely (e.g. `~/.sagittarius/google-chat-key.json`). Ensure file permissions are restrictive:
   ```bash
   chmod 600 ~/.sagittarius/google-chat-key.json
   ```

---

## Configuration in Sagittarius

Configure the bridge in `<repo>/.sagittarius/settings.json` (project scope) or `~/.sagittarius/settings.json` (global scope):

```json
{
  "sagittarius": {
    "chat": {
      "googleChat": {
        "enabled": true,
        "spaceId": "spaces/AAAAAAAAAAA",
        "authorizedUsers": [
          "users/10293847561029384756",
          "rob@example.com"
        ],
        "projectId": "my-sagittarius-project",
        "subscriptionId": "sagittarius-chat-sub",
        "credentialsFile": "~/.sagittarius/google-chat-key.json",
        "maxResultRunes": 2000,
        "confirmTimeout": 300
      }
    }
  }
}
```

### Settings Reference

| Setting | Type | Default | Description |
|---|---|---|---|
| `sagittarius.chat.googleChat.enabled` | bool | `false` | Enable Google Chat integration |
| `sagittarius.chat.googleChat.spaceId` | string | `""` | Target 1:1 DM Space ID (e.g. `spaces/AAAA...`) |
| `sagittarius.chat.googleChat.authorizedUsers` | string[] | `[]` | Allowed user resource names (`users/<id>`) and/or emails |
| `sagittarius.chat.googleChat.projectId` | string | `""` | GCP Project ID hosting Pub/Sub |
| `sagittarius.chat.googleChat.subscriptionId` | string | `""` | Pub/Sub pull subscription ID |
| `sagittarius.chat.googleChat.credentialsFile` | string | `""` | Path to service account JSON key (accepts `~` paths; falls back to ADC if empty) |
| `sagittarius.chat.googleChat.maxResultRunes` | int | `2000` | Max character length for tool outputs posted to chat |
| `sagittarius.chat.googleChat.confirmTimeout` | int | `300` | Seconds before an interactive confirmation card expires and denies |

---

## Running Sagittarius with Google Chat

### Combined Mode (TUI + Google Chat)
Run Sagittarius normally with `--google-chat`:
```bash
sagittarius --google-chat
```
Both the terminal TUI and the Google Chat DM will be active simultaneously.

### Headless Standalone Service (`--google-chat-only`)
Run as a background daemon with no terminal required:
```bash
sagittarius --google-chat-only
```

#### Systemd User Service (`~/.config/systemd/user/sagittarius-chat.service`):
```ini
[Unit]
Description=Sagittarius Google Chat Bridge
After=network.target

[Service]
Type=simple
WorkingDirectory=/home/rob/src/my-project
ExecStart=/home/rob/bin/sagittarius --google-chat-only
Restart=always
RestartSec=5
Environment=PATH=/usr/local/bin:/usr/bin:/home/rob/bin

[Install]
WantedBy=default.target
```
Enable and start the service:
```bash
systemctl --user daemon-reload
systemctl --user enable --now sagittarius-chat.service
```

---

## Security Model

1. **DM Only (`singleUserBotDm`):** Group Spaces are refused. The bot only engages in private 1:1 DMs.
2. **Authorized Sender Check:** Every incoming message and card button click is verified against `authorizedUsers`. Unauthorized messages and clicks are dropped silently.
3. **Interactive Approvals:** Destructive tools (`write_file`, mutating shell commands) generate interactive cards with "Allow once", "Allow session", and "Deny" buttons.
4. **No YOLO Mode:** Running with `--yolo` or `-y` is rejected outright when Google Chat is enabled.
5. **Fail-Closed Confirmation Timeout:** If an approval card is not answered within `confirmTimeout` seconds, it automatically denies execution.
6. **Tool Output Redaction:** Bearer tokens, private keys, and API keys are automatically redacted from tool output before being posted to Google Chat.
7. **Stop Control:** Every turn features a live status card with a "Stop" button, and users can send `/stop` at any time to cancel in-flight model generation, tool execution, and any prompts queued behind it, allowing immediate re-prompting. Terminal turns cross-echo to the DM with full answer mirroring on completion.
