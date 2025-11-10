# ircpush
Colorizing message forwarder for IRC.

ircpush connects to an IRC server and forwards messages received from local inputs (e.g. TCP, rsyslog). It supports per‑channel regex / word highlighting, IRC colors, auto‑reconnect, configurable message splitting and truncation.

## Download (preferred)
Get prebuilt binaries from GitHub Releases:
https://github.com/bitcanon/ircpush/releases

Example (Linux amd64):
```bash
curl -L -o ircpush.tgz https://github.com/bitcanon/ircpush/releases/download/v1.0.4/ircpush-linux-amd64-v1.0.4.tgz
tar -xzf ircpush.tgz
sudo install -m 0755 ircpush /usr/local/bin/ircpush
ircpush --version
```

## Build from source
```bash
go build -o ircpush ./...
```

## Configuration
Search order:
1. ./config.yaml (working directory)
2. ~/.ircpush          (YAML file)
3. /etc/ircpush/config.yaml

Example:
```yaml
tcp:
  listen: "10.20.30.40:9000"
  max_line_bytes: 65536         # drop incoming lines over this byte size (0 = default 65536)

irc:
  server: "irc.example.net:6697"
  tls: true
  tls_skip_verify: false
  nick: "ircbot"
  realname: "ircbot"
  channels: ["#network", "#server", "#security"]
  max_message_len: 400          # 0 = unlimited (after highlighting)
  split_long: true              # true = split into multiple PRIVMSG, false = truncate + "..."

highlight:
  auto_reload: true
  rules:
    # Drop keyword (whole line highlight)
    - kind: regex
      pattern: "(?i)\\bdrop\\b"
      color: "04,01"
      bold: true
      whole_line: true
      channels: ["#security"]

    # IPv4:port (color only port)
    - kind: regex
      pattern: "\\b(?P<ip>(?:\\d{1,3}\\.){3}\\d{1,3}):(?P<port>\\d{1,5})\\b"
      color: brown
      groups: ["port"]

    # in:/out: interface values (single rule, color only value)
    - kind: regex
      pattern: "(?i)(?:\\bin:(?P<in>[^, ]+)|\\bout:(?P<out>\\([^)]*\\)|[^, ]+))"
      color: grey
      groups: ["in","out"]
```

## CLI test client
```bash
ircpush client --config ./config.yaml
```
- Prefix “#channel ” or “#a,#b ” to target channels.
- /quit to exit.

## Run TCP listener
```bash
ircpush serve --config ./config.yaml
```
Line targeting:
- "#server message"        -> only #server
- "#a,#b msg"              -> #a and #b
- Otherwise                -> all configured channels

### rsyslog template example
```
template(name="IRCHAProxy" type="string" string="#server %hostname%: %programname%%msg%\n")
if ($programname == 'haproxy' and not re_match($msg, '[0-9]+/[0-9]+/[0-9]+/[0-9]+/[0-9]+ 200')) then {
  @@10.20.30.40:9000;IRCHAProxy
}
```

## systemd service
Sample unit: systemd/ircpush.service

Install:
```bash
sudo install -m 0755 ircpush /usr/local/bin/ircpush

sudo groupadd --system ircpush || true
sudo useradd  --system --no-create-home --home /var/lib/ircpush \
  --shell /usr/sbin/nologin --gid ircpush ircpush || true

sudo install -d -o ircpush -g ircpush /var/lib/ircpush
sudo install -d -o root   -g root   /etc/ircpush
sudo cp ./config.yaml /etc/ircpush/config.yaml
sudo cp ./systemd/ircpush.service /etc/systemd/system/ircpush.service
sudo systemctl daemon-reload
sudo systemctl enable --now ircpush.service
systemctl status ircpush.service
```

Optional env overrides (/etc/default/ircpush):
```bash
IRCPUSH_TCP_LISTEN="0.0.0.0:9000"
IRCPUSH_IRC_TLS="true"
```

Test:
```bash
echo '#server hello from nc' | nc -q 0 10.20.30.40 9000
```

## Highlighting rules (extended)
Fields per rule:
- kind: regex | word
- pattern: pattern string (regex or literal word)
- color: IRC color (fg[,bg]) or name (lightgreen, brown, grey, cyan, etc.)
- bold / underline: booleans
- whole_line: color full line instead of just match
- channels / exclude_channels: scope (match prefixes & wildcards if implemented)
- groups: list of named or numeric capture groups to color (regex only)
- auto_reload: when true on highlight root, rules reload on file save

Ordering: Place specific group-sensitive rules (e.g. IPv4:port) before broader matches (generic IPv4) to avoid consuming or altering the text.

Group coloring example:
```yaml
- kind: regex
  pattern: "\\bproto\\s+(?P<proto>tcp|udp|icmp)\\b"
  color: teal
  groups: ["proto"]
```

## Message length & limits
Stages:
1. tcp.max_line_bytes (bytes): lines exceeding this are dropped (scanner error).
2. Highlighting: may add control codes (increasing byte count).
3. irc.max_message_len (runes) + split_long:
   - split_long: true -> segment into multiple PRIVMSG (try to break on space).
   - split_long: false -> truncate; if limit > 3 append "...".
Configure tcp.max_line_bytes >= (max_message_len + highlighting overhead) to avoid unintended drops.

## Info / diagnostics
Use:
```bash
ircpush info --config /etc/ircpush/config.yaml
```
Shows:
- Config file path
- IRCPUSH_* env vars
- Effective merged config
- Keys overridden by env/flags

Helps detect mismatches (e.g. TLS forced on plaintext port).

## Troubleshooting
Issue: Server logs “Client unregistered … Timeout” with in>0, out=0.
Cause: TLS/plaintext mismatch or missing NICK/USER due to early client failure.
Check:
```bash
nc -v irc.example.net 6667
printf 'NICK x\r\nUSER x x x :x\r\n' | nc -v irc.example.net 6667
openssl s_client -connect irc.example.net:6697 -servername irc.example.net
```

Use tcpdump:
```bash
sudo tcpdump -n -vv -X port 6667 -c 20
```
Leading bytes 16 03 01 indicate TLS handshake sent to plaintext port.

## Reloading
- Highlight rules: auto if highlight.auto_reload: true.
- Structural changes (tcp.listen, IRC server): restart service.

## Versioning
Binary reads ./version file (or falls back to dev). Release tags match its content.

## License
MIT.
