# Vendored jumpserver-dev/sdk-go (XPAM patches)

Upstream version: v0.0.0-20260826024103-e9e4992f746b (from proxy.golang.org).
Used via `replace github.com/jumpserver-dev/sdk-go => ./third_party/sdk-go` in koko's go.mod.

Patches:
- model/asset.go: `Asset.Info` (`json:"info"`) so koko receives the asset info that core already
  sends in the connection token (used for native RDP RemoteApp: remote_app / remote_app_dir /
  remote_app_args).

When upgrading koko, re-vendor the SDK version pinned by upstream and re-apply these patches.
