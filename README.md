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

Capture login (OTP in terminal by default):

```bash
bislericli auth login
```

## Usage

Place an order (default: 2 jars, return 2 empty jars):

```bash
bislericli order
```

If the saved session is expired, `order` now prompts:

- `Session expired. Would you like to log in now? [y/N]`
- if no answer within 10 seconds, it exits and asks you to run `bislericli auth login` manually

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

During OTP login, type `r` at the OTP prompt to request a new OTP.

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
