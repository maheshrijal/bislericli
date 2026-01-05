# bislericli (unofficial CLI)

Unofficial CLI to place Bisleri 20L jar orders quickly. Not affiliated with or endorsed by Bisleri.

## Requirements

- Bisleri account and **preloaded Bisleri Wallet**.
- Default delivery address configured on bisleri.com.
- Go 1.22+ to build.

### Install via Homebrew

```bash
brew install maheshrijal/tap/bislericli
```

### Build from source

```bash
go build -o bislericli ./cmd/bislericli
```

Capture login (opens Chrome, you log in manually):

```bash
bislericli auth login
```

## Usage

Place an order (default: 2 jars, return 2 empty jars):

```bash
bislericli order
```

Override defaults:

```bash
bislericli order --qty 3 --return 1
```

Allow order if other cart items exist:

```bash
bislericli order --allow-extra
```

Check auth status:

```bash
bislericli auth status
```

List profiles:

```bash
bislericli profile list
```

Set current profile:

```bash
bislericli profile use personal
```

Sync order history (caches data locally):

```bash
bislericli sync
```

View order history (from cache or live):

```bash
bislericli orders --limit 5
```

Analyze spending habits:

```bash
bislericli stats
```

View ordering patterns (day/time):

```bash
bislericli stats --view-patterns
```

Show config location:

```bash
bislericli config show
```

## Configuration

Config is stored in:

- macOS: `~/Library/Application Support/bislericli/`
- Linux: `$XDG_CONFIG_HOME/bislericli/` (or `~/.config/bislericli/`)

Files:

- `config.json` (global defaults, current profile)
- `profiles/<name>.json` (cookies + address)

## Notes

- The CLI uses the **Bisleri Wallet** payment method. Ensure your wallet is funded; otherwise the order will fail.
- If no default address is detected, the CLI will prompt you to choose one and fill any missing fields.
- This tool avoids CAPTCHA/waf triggers by throttling requests.

## Security Note

> [!WARNING]
> Session cookies are stored in **plaintext JSON files** in your configuration directory (`~/Library/Application Support/bislericli/profiles/`).
> Ensure your computer is secure and do not share these files. Support for OS-native keychain storage is planned.

