# GUI Concept

This document describes the planned graphical interface for KVM Java Launcher.
The current CLI behavior should remain available; the GUI will become a safer
and more convenient operator layer over the existing launch engine.

## Goals

- Provide a two-pane desktop interface for creating, launching, saving, and
  reusing BMC/KVM connection profiles.
- Keep all existing vendor launch behavior available: AMI, Supermicro,
  Supermicro SOL, Dell iDRAC, HPE iLO, Lenovo, generic discovery, and local JNLP.
- Store saved connections in an encrypted local connection book under AppData.
- Make stored connections searchable by name, group, host/IP, port, username,
  vendor profile, and notes.
- Keep one-off launches possible even when no connection book is unlocked.
- Avoid hiding important lifecycle actions: time rollback/restore, logout,
  stunnel diagnostics, Java output, and launch errors must be visible in logs.

## Layout

The main window is split into two primary areas.

The selected concept is the vertical library layout:

- Left side: connection editor, launch actions, and session log.
- Right side: encrypted saved connection book with search and grouped entries.
- The left and right areas scroll independently from each other.
- The main window itself should avoid page-level scrolling during normal desktop
  use.

The layout must remain usable on a 1280 x 960 display. Large monitors and
high-DPI displays should not require operating-system scaling changes just to
make the launcher comfortable.

### Connection Editor

The left side contains all settings for the selected or new connection.

Expected fields:

- Connection name.
- Group name.
- BMC target host/IP/URL.
- Optional port or full URL.
- Optional Host header override for NAT.
- Username.
- Password.
- Vendor profile:
  - auto
  - ami
  - supermicro
  - supermicro-sol
  - dell
  - hp
  - lenovo
  - generic
  - jnlp
- Local JNLP file path, enabled for `jnlp` profile.
- Supermicro certificate mode:
  - noverify
  - renew-client
  - direct
  - off
- Supermicro time workaround:
  - off
  - auto
- Optional notes.

Expected actions:

- Connect.
- Save.
- Save as new.
- Delete.
- Duplicate.
- Test login/discovery.
- Browse local JNLP.
- Clear form.

The editor should also show a launch log panel or expandable log drawer. The log
must include the same high-signal text currently printed by the CLI.

### Saved Connections

The right side contains the encrypted connection book.

Expected controls:

- Search box.
- Grouped list/tree by group name.
- Ungrouped section for entries without a group.
- Sort by name, group, host, profile, or last used.
- Optional filter chips for vendor profile.

Search should match:

- Connection name.
- Group.
- Host/IP/URL.
- Port.
- Username.
- Vendor profile.
- Notes.

## Appearance And Scaling

The GUI should include application-level appearance settings.

Theme options:

- Light.
- Dark.
- System default.

The theme should affect the whole application, including the connection book,
editor, modal dialogs, and logs. The session log can keep a terminal-like dark
background in both themes if that remains easier to read.

Font and density options:

- UI scale or font scale, for example 85% to 140%.
- Compact, normal, and comfortable density presets if the chosen GUI toolkit
  supports them cleanly.
- Persist the last selected scale/theme in a small non-secret settings file.

The scale setting is separate from the encrypted connection book password. It is
not secret and should load before the book is unlocked.

Responsive behavior:

- At wide widths, show editor and saved list at the same time.
- At medium widths, keep the log visible without forcing horizontal scrolling.
- At narrow widths, stack sections and keep primary actions reachable near the
  top.
- Avoid fixed-size controls that truncate long hostnames, group names, or JNLP
  paths without a tooltip or secondary detail line.

List item display:

- Name as the primary label.
- Host/IP and profile as secondary details.
- Optional badges for NAT Host header, local JNLP, Supermicro time workaround,
  or saved password.

## Connection Book

The connection book is created on first saved connection.

Suggested location:

```text
%APPDATA%\KVMJavaLauncher\connections.kvmb
```

The file should not be created during one-off launches. It is created only when
the user saves the first connection.

Startup behavior:

- If the connection book does not exist, open the GUI with an empty saved list.
- If the connection book exists, prompt for the decryption password.
- If the password is correct, load saved connections.
- If the user cancels, continue without loading saved connections.
- If the user cancels or decryption fails, saving must be disabled until the book
  is unlocked or a new book is explicitly created.

## Encryption

Requirements:

- Do not store the encryption password.
- Do not store plaintext passwords outside memory.
- Encrypt the entire connection book, not just password fields.
- Use authenticated encryption so corruption or wrong passwords are detected.
- Use streaming or bounded-memory handling if the file grows, but the expected
  file size is small enough that full-file encryption is acceptable initially.

Recommended design:

- Key derivation: Argon2id.
- Encryption: XChaCha20-Poly1305 or AES-256-GCM.
- Random salt per connection book.
- Random nonce per save.
- Store KDF parameters in the unencrypted file header.
- Store ciphertext and authentication tag after the header.

Suggested header fields:

```json
{
  "magic": "KVMBOOK",
  "version": 1,
  "kdf": "argon2id",
  "kdf_params": {
    "time": 3,
    "memory_kib": 65536,
    "threads": 2
  },
  "cipher": "xchacha20-poly1305",
  "salt": "base64"
}
```

The decrypted payload should be JSON so it remains easy to migrate:

```json
{
  "version": 1,
  "connections": []
}
```

## Saved Connection Model

Initial model:

```json
{
  "id": "uuid",
  "name": "Server room R730xd",
  "group": "Customer A",
  "target": "192.168.7.70",
  "host_header": "",
  "username": "root",
  "password": "encrypted inside book payload",
  "profile": "dell",
  "jnlp_path": "",
  "supermicro_cert_mode": "noverify",
  "supermicro_time_workaround": "off",
  "notes": "",
  "created_at": "2026-08-22T00:00:00Z",
  "updated_at": "2026-08-22T00:00:00Z",
  "last_used_at": "2026-08-22T00:00:00Z"
}
```

Because the whole book is encrypted, individual password fields do not need a
second layer of encryption in version 1.

## Launch Lifecycle

The GUI should use the same launch lifecycle as the CLI.

Expected order:

1. Prepare embedded Java 8.
2. Login/discover/download JNLP or parse local JNLP.
3. Download jars/native resources.
4. Apply vendor-specific workarounds if needed.
5. Start Java KVM.
6. Stream Java output into the GUI log.
7. Wait for Java process exit.
8. Print diagnostics such as stunnel logs.
9. Restore BMC time if the Supermicro time workaround changed it.
10. Logout/close BMC web session best-effort.
11. Update `last_used_at` if the connection book is unlocked.

If a step fails, cleanup still runs where applicable.

## Local JNLP Mode

The GUI must allow launching a downloaded JNLP file manually.

Behavior:

- User selects profile `jnlp`.
- UI asks for a local `.jnlp` path.
- Launcher parses the file and displays:
  - codebase
  - application or applet type
  - main class
  - resources
  - arguments with sensitive values masked
- If target/user/password are also provided, use them for authenticated resource
  downloads where possible.
- If the JNLP references expired ATEN/Supermicro certificates, show the same
  warnings and available workarounds as vendor mode.

This is intended for any BMC where the web UI can download a JNLP/SOL/console
file but automatic discovery is not yet implemented.

## Security UX

The GUI should make the following states explicit:

- Connection book locked.
- Connection book unlocked.
- Saving disabled because unlock was cancelled.
- Saved password present.
- Launch uses temporary BMC time rollback.
- BMC time restore succeeded or failed.
- Logout succeeded or was not confirmed.

Passwords:

- Password fields should be masked by default.
- Provide a reveal button while pressed.
- Never print passwords to logs.
- Mask known sensitive JNLP arguments.

## Implementation Notes

Potential GUI stack options:

- Keep Go and use Wails for a native-feeling Windows app with a web frontend.
- Use Fyne for a pure Go desktop UI.
- Keep CLI binary as the launcher engine and build a separate GUI wrapper.

Recommended direction:

- Refactor current CLI launch flow into an internal service API first.
- Keep `cmd/kvm-launcher` as a CLI frontend.
- Add a new `cmd/kvm-launcher-gui` or replace the entrypoint after the service
  API is stable.
- Avoid mixing GUI state directly into vendor-specific BMC logic.

## Open Questions

- Should saved passwords be optional per connection?
- Should the connection book auto-lock after inactivity?
- Should the GUI support multiple simultaneous KVM sessions?
- Should import/export be supported for encrypted books?
- Should there be a portable mode where the encrypted book lives next to the exe?
- Should the local JNLP profile allow non-`.jnlp` files if the content is XML
  JNLP despite a different extension?
