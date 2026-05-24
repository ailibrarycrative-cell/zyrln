# Android Setup

The Android app runs the relay proxy directly on your phone — no desktop needed once it's configured.

## Before You Start

Complete the relay setup first:
- [Exit relay running](../README.md#step-2--deploy-the-exit-relay) (VPS or Cloudflare)
- [Apps Script deployed](../README.md#step-3--deploy-the-apps-script-relay)

## Install the App

Requires Android 9 (API 29) or higher.

Download the APK from the [Releases](../../releases) page and install it on your phone.

If Android blocks the install: **Settings → Install unknown apps** → allow your file manager.

## First-Time Setup

### 1. Import your config

The easiest way is from the desktop app:

1. In the desktop app, click the **export** (download) button in the Tools section
2. Copy the JSON that appears
3. On your phone, tap **Import Config from Clipboard**

The config is saved automatically. You can import multiple configs from different relay setups and switch between them.

**If you don't have the desktop app**, create the JSON manually:

```json
{"url":"https://script.google.com/macros/s/YOUR_ID/exec","key":"YOUR_AUTH_KEY"}
```

For multiple Apps Script URLs (better resilience):

```json
{"url":"https://script.google.com/.../exec1,https://script.google.com/.../exec2","key":"YOUR_AUTH_KEY"}
```

### 2. Connect

1. Tap a config in the list to select it (it highlights)
2. Tap the **connect** button (power icon, top right)
3. Allow VPN permission when Android asks
4. The button turns green — you're connected

To disconnect: tap the connect button again.

No CA certificate is needed — Android uses a TCP-over-HTTP tunnel, not HTTPS MITM.

## Direct Mode (Google Services)

The **⚡ lightning bolt** button enables direct mode — Google services (Gmail, Drive, Maps, Docs) connect directly to Google with fragmented TLS instead of going through the relay. Faster and uses less quota. YouTube video streaming and Play Store downloads still go through the relay.

Enable it by tapping the lightning bolt (it turns green). Works independently from the relay — you can have both active at once.

## Ping

Tap **ping** to measure relay round-trip time. Uses the currently selected or active config. Useful for comparing multiple relay deployments.

## Building the APK Yourself

Requires Android Studio with NDK installed.

```bash
# Install gomobile (once)
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init

# Build
make keystore        # generate signing key (once)
make android         # signed release APK
```

APK location: `android/app/build/outputs/apk/release/`

## Troubleshooting

**Some apps don't work through the proxy**
Apps that hardcode their own TLS certificates (banking apps, some payment apps) ignore the system proxy. This cannot be fixed without root access.

**VPN permission denied**
Go to **Settings → Apps → Zyrln → Permissions** and grant VPN access manually.

**Config not connecting**
Run the desktop **Diagnostics** tool to verify the relay chain is working before importing to Android.
