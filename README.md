# ircpush
Colorizing message forwarder for IRC

ircpush connects to an IRC server and forwards messages received from local inputs (e.g., TCP from rsyslog). It supports per-channel, regex/word-based highlighting with IRC colors, and auto-reconnect.

## Build
```bash
go build -o ircpush ./...
```

## Configuration
The app searches config in this order:
1) ./config.yaml (next to the executable)
2) ~/.ircpush (YAML content)
3) /etc/ircpush/config.yaml

Example:
```yaml
tcp:
  listen: "10.20.30.40:9000"

irc:
  server: "irc.example.net:6697"
  tls: true
  tls_skip_verify: false
  nick: "ircbot"
  realname: "ircbot"
  channels: ["#network", "#server", "#security"]

highlight:
  rules:
    - kind: regex
      pattern: "(?i)\\bdrop\\b"
      color: "04,01"
      bold: true
      whole_line: true
      channels: ["#security"]
```

## CLI test client
```bash
./ircpush client -c ./config.yaml
```
- Type messages; prefix with “#channel ” to target it (e.g. “#security hello”).
- /quit to exit.

## Run TCP listener
```bash
./ircpush serve -c ./config.yaml
```
- Binds to tcp.listen, reads one line per message.
- Lines may start with “#channel ” or “#a,#b ” to target channels; otherwise they’re broadcast.

### rsyslog example
```
template(name="IRCHAProxy" type="string" string="#server %hostname%: %programname%%msg%\n")
if ($programname == 'haproxy' and not re_match($msg, '[0-9]+/[0-9]+/[0-9]+/[0-9]+/[0-9]+ 200')) then {
  @@10.20.30.40:12345;IRCHAProxy
}
```

## systemd service (Debian/Ubuntu)
A sample unit is provided in systemd/ircpush.service.

Install:
```bash
# Install binary
sudo install -m 0755 ircpush /usr/local/bin/ircpush

# Create user/group and dirs
sudo groupadd --system ircpush || true
sudo useradd --system --no-create-home --home /var/lib/ircpush --shell /usr/sbin/nologin --gid ircpush ircpush || true
sudo install -d -o ircpush -g ircpush -m 0755 /var/lib/ircpush
sudo install -d -o root -g root -m 0755 /etc/ircpush

# Config
sudo cp ./config.yaml /etc/ircpush/config.yaml

# Unit
sudo cp ./systemd/ircpush.service /etc/systemd/system/ircpush.service
sudo systemctl daemon-reload
sudo systemctl enable --now ircpush.service
systemctl status ircpush.service
```

Optional env overrides:
```bash
# /etc/default/ircpush
IRCPUSH_TCP_LISTEN="10.20.30.40:9000"
```

Test listener:
```bash
echo '#server hello from nc' | nc 10.20.30.40 9000
```
